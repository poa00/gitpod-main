// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/gitpod-io/gitpod/common-go/log"
)

type S3Config struct {
	Bucket       string        `yaml:"bucket"`
	Region       string        `yaml:"region"`
	PollInterval time.Duration `yaml:"pollInterval,omitempty"`
}

var _ Transport = &S3Transport{}

type S3Transport struct {
	Config *S3Config

	s3 *s3.Client
}

func NewS3Transport(config *S3Config) (*S3Transport, error) {
	// Load the Shared AWS Configuration (~/.aws/config)
	cfg, err := aws_config.LoadDefaultConfig(context.TODO(), aws_config.WithRegion(config.Region))
	if err != nil {
		log.Fatal(err)
	}

	return &S3Transport{
		Config: config,
		s3:     s3.NewFromConfig(cfg),
	}, nil
}

func (t *S3Transport) CreateSession(ctx context.Context, sessionId string) error {
	_, err := t.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &t.Config.Bucket,
		Key:    aws.String(t.sessionsPath(sessionId)),
	})
	return err
}

func (t *S3Transport) HasSession(ctx context.Context, sessionId string) bool {
	head, err := t.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &t.Config.Bucket,
		Key:    aws.String(t.sessionsPath(sessionId)),
	})

	return err == nil && head != nil
}

func (t *S3Transport) WatchSessions(ctx context.Context) (<-chan string, error) {
	out := make(chan string, 10)

	existingSessions := map[string]struct{}{}
	readAllSessions := func() error {
		listResp, err := t.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &t.Config.Bucket,
			Prefix: aws.String(t.sessionsPath()),
		})
		if err != nil {
			return fmt.Errorf("error listing session objects: %w", err)
		}

		for _, obj := range listResp.Contents {
			if path.Dir(*obj.Key) != t.sessionsPath() {
				continue
			}
			sessionId := path.Base(*obj.Key)
			if _, exists := existingSessions[sessionId]; exists {
				continue
			}
			out <- sessionId
			existingSessions[sessionId] = struct{}{}
		}
		return nil
	}

	// send initial list of sessions (nice to see everything is working)
	err := readAllSessions()
	if err != nil {
		return nil, err
	}

	go repeatUntilDone(ctx, func() {
		err := readAllSessions()
		if err != nil {
			log.WithError(err).Error("cannot read sessions")
		}
	}, t.Config.PollInterval, func() {
		close(out)
	})

	return out, nil
}

func (t *S3Transport) WatchRequests(ctx context.Context, sessionId string) (<-chan *Message, error) {
	log := log.WithField("sessionId", sessionId)

	out := make(chan *Message, 10)
	pushRequest := func(reqID int) {
		fn := t.requestPath(sessionId, reqID)
		obj, err := t.s3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &t.Config.Bucket,
			Key:    aws.String(fn),
		})
		if err != nil {
			log.WithError(err).WithField("file", fn).Error("cannot read request object")
			return
		}

		var stdBuffer bytes.Buffer
		defer obj.Body.Close()
		_, err = io.Copy(&stdBuffer, obj.Body)
		if err != nil {
			log.WithError(err).WithField("file", fn).Error("cannot read body of request object")
		}

		m := Message{
			ID:   reqID,
			Data: stdBuffer.Bytes(),
		}
		out <- &m
	}

	forwardAllUnansweredRequests := func() error {
		listResp, err := t.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &t.Config.Bucket,
			Prefix: aws.String(t.sessionPath(sessionId)),
		})
		if err != nil {
			return fmt.Errorf("error listing request objects: %w", err)
		}

		allRequests := map[int]string{}
		allResponses := map[int]string{}
		for _, obj := range listResp.Contents {
			if path.Dir(*obj.Key) != t.sessionPath(sessionId) {
				continue
			}
			name := path.Base(*obj.Key)
			reqId, err := parseRequestIdFromFilename(name)
			if err == nil {
				if _, hasResponse := allResponses[reqId]; hasResponse {
					continue
				}
				allRequests[reqId] = name
				continue
			}
			reqId, err = parseResponseIdFromFilename(name)
			if err == nil {
				allResponses[reqId] = name
				delete(allRequests, reqId)
				continue
			}
		}

		for reqId := range allRequests {
			pushRequest(reqId)
		}
		return nil
	}

	log.Info("pushing initial requests")
	err := forwardAllUnansweredRequests()
	if err != nil {
		return nil, err
	}

	go repeatUntilDone(ctx, func() {
		err := forwardAllUnansweredRequests()
		if err != nil {
			log.WithError(err).Error("error reading new requests")
		}
	}, t.Config.PollInterval, func() {
		close(out)
	})

	return out, err
}

func (t *S3Transport) GetLastRequestID(ctx context.Context, sessionId string) (int, error) {
	listResp, err := t.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &t.Config.Bucket,
		Prefix: aws.String(t.sessionPath(sessionId)),
	})
	if err != nil {
		return 0, fmt.Errorf("error listing request objects: %w", err)
	}

	var lastRequestID int
	for _, obj := range listResp.Contents {
		if path.Dir(*obj.Key) != t.sessionPath(sessionId) {
			continue
		}
		name := path.Base(*obj.Key)
		reqID, err := parseRequestIdFromFilename(name)
		if err != nil {
			continue
		}

		if reqID > lastRequestID {
			lastRequestID = reqID
		}
	}

	return lastRequestID, nil
}

func (t *S3Transport) SendUnary(ctx context.Context, sessionId string, req *Message) (*Message, error) {
	_, err := t.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &t.Config.Bucket,
		Key:    aws.String(t.requestPath(sessionId, req.ID)),
		Body:   bytes.NewReader(req.Data),
	})
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}

	bytes, err := t.waitForResponse(ctx, sessionId, req.ID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for response: %w", err)
	}

	resp := Message{
		ID:   req.ID,
		Data: bytes,
	}
	return &resp, nil
}

func (t *S3Transport) waitForResponse(ctx context.Context, sessionId string, reqId int) ([]byte, error) {
	resPath := t.responsePath(sessionId, reqId)

	doRead := func() ([]byte, error) {
		obj, err := t.s3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &t.Config.Bucket,
			Key:    aws.String(resPath),
		})
		if err == nil {
			var stdBuffer bytes.Buffer
			defer obj.Body.Close()
			_, err = io.Copy(&stdBuffer, obj.Body)
			if err != nil {
				return nil, fmt.Errorf("error reading body of response object: %w", err)
			}
			return stdBuffer.Bytes(), nil
		}

		var ae smithy.APIError
		if !errors.As(err, &ae) || ae.ErrorCode() != "NoSuchKey" {
			log.WithError(err).WithField("ae", ae.ErrorCode()).Error("cannot read response object")
			return nil, fmt.Errorf("error reading response object: %w", err)
		}
		return nil, nil
	}

	ticker := time.NewTicker(t.Config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for response")
		case <-ticker.C:
			bytes, err := doRead()
			if err != nil {
				return nil, err
			}
			if bytes == nil {
				ticker.Reset(t.Config.PollInterval)
				continue
			}
			return bytes, nil
		}
	}
}

func (t *S3Transport) SendResponse(ctx context.Context, sessionId string, msg *Message) error {
	_, err := t.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &t.Config.Bucket,
		Key:    aws.String(t.responsePath(sessionId, msg.ID)),
		Body:   bytes.NewReader(msg.Data),
	})
	return err
}

func (t *S3Transport) SendStream(ctx context.Context, sessionId string, msg *Message) (<-chan *Message, error) {
	return nil, fmt.Errorf("not implemented")
}

func (t *S3Transport) sessionsPath(parts ...string) string {
	ps := []string{"sessions"}
	ps = append(ps, parts...)
	return path.Join(ps...)
}

func (t *S3Transport) sessionPath(sessionId string, parts ...string) string {
	ps := []string{"session", sessionId}
	ps = append(ps, parts...)
	return path.Join(ps...)
}

func (t *S3Transport) requestPath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-req.yaml", reqId))
}

func (t *S3Transport) responsePath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-res.yaml", reqId))
}
