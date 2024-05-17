// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gitpod-io/gitpod/common-go/log"
)

const DEFAULT_POLL_INTERVAL = 2 * time.Second

type FSConfig struct {
	Root         string        `yaml:"root"`
	PollInterval time.Duration `yaml:"pollInterval,omitempty"`
}

var _ Transport = &FSTransport{}

type FSTransport struct {
	Config *FSConfig
}

func NewFSTransport(cfg *FSConfig) (*FSTransport, error) {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DEFAULT_POLL_INTERVAL
	}

	return &FSTransport{
		Config: cfg,
	}, nil
}

func (t *FSTransport) CreateSession(ctx context.Context, sessionId string) error {
	// create the session dir
	_, err := os.Stat(t.sessionPath(sessionId))
	if err == nil {
		// there already is a file or directory with that name
		return fmt.Errorf("session already exists: %s", sessionId)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.MkdirAll(t.sessionPath(sessionId), 0755)
}

func (t *FSTransport) HasSession(ctx context.Context, sessionId string) bool {
	// test if session dir exists
	i, err := os.Stat(t.sessionPath(sessionId))
	return err == nil && i.IsDir()
}

func (t *FSTransport) WatchSessions(ctx context.Context) (<-chan string, error) {
	out := make(chan string, 10)

	existingSessions := map[string]struct{}{}
	readAllSessions := func() error {
		entries, err := os.ReadDir(t.sessionsPath())
		if err != nil {
			return fmt.Errorf("cannot read sessions directory: %w", err)
		}

		for _, entry := range entries {
			_, exists := existingSessions[entry.Name()]
			if !entry.IsDir() || exists {
				continue
			}
			out <- entry.Name()
			existingSessions[entry.Name()] = struct{}{}
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

func repeatUntilDone(ctx context.Context, f func(), period time.Duration, done func()) {
	defer done()

	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f()
			ticker.Reset(period) // avoid drift
		}
	}
}

func (t *FSTransport) WatchRequests(ctx context.Context, sessionId string) (<-chan *Message, error) {
	log := log.WithField("sessionId", sessionId)

	out := make(chan *Message, 10)
	pushRequest := func(reqID int) {
		fn := t.requestPath(sessionId, reqID)
		bytes, err := os.ReadFile(fn)
		if err != nil {
			log.WithError(err).WithField("file", fn).Error("cannot read request file")
			return
		}
		m := Message{
			ID:   reqID,
			Data: bytes,
		}
		out <- &m
	}

	forwardAllUnansweredRequests := func() error {
		entries, err := os.ReadDir(t.sessionPath(sessionId))
		if err != nil {
			return fmt.Errorf("cannot read sessions directory: %w", err)
		}

		allRequests := map[int]string{}
		allResponses := map[int]string{}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			reqId, err := parseRequestIdFromFilename(entry.Name())
			if err == nil {
				if _, hasResponse := allResponses[reqId]; hasResponse {
					continue
				}
				allRequests[reqId] = entry.Name()
				continue
			}
			reqId, err = parseResponseIdFromFilename(entry.Name())
			if err == nil {
				allResponses[reqId] = entry.Name()
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

func (t *FSTransport) SendUnary(ctx context.Context, sessionId string, req *Message) (*Message, error) {
	if !t.HasSession(ctx, sessionId) {
		return nil, fmt.Errorf("session does not exist")
	}

	// write data to file
	reqFileName := t.requestPath(sessionId, req.ID)
	err := os.WriteFile(reqFileName, req.Data, 0644)
	if err != nil {
		return nil, fmt.Errorf("error writing request: %w", err)
	}

	// wait for response
	bytes, err := t.waitForResponse(ctx, sessionId, req.ID)
	if err != nil {
		return nil, fmt.Errorf("error receiving for response: %w", err)
	}

	resp := Message{
		ID:   req.ID,
		Data: bytes,
	}
	return &resp, nil
}

func (t *FSTransport) SendResponse(ctx context.Context, sessionId string, msg *Message) error {
	if !t.HasSession(ctx, sessionId) {
		return fmt.Errorf("session does not exist")
	}

	// write data to file
	resFileName := t.responsePath(sessionId, msg.ID)
	err := os.WriteFile(resFileName, msg.Data, 0644)
	if err != nil {
		return fmt.Errorf("error writing response: %w", err)
	}
	return nil
}

func (t *FSTransport) GetLastRequestID(ctx context.Context, sessionId string) (int, error) {
	if !t.HasSession(ctx, sessionId) {
		return 0, fmt.Errorf("session does not exist")
	}

	entries, err := os.ReadDir(t.sessionPath(sessionId))
	if err != nil {
		return 0, fmt.Errorf("cannot read session directory: %w", err)
	}

	var lastRequestID int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		reqID, err := parseRequestIdFromFilename(entry.Name())
		if err != nil {
			continue
		}

		if reqID > lastRequestID {
			lastRequestID = reqID
		}
	}

	return lastRequestID, nil
}

func (t *FSTransport) waitForResponse(ctx context.Context, sessionId string, reqId int) ([]byte, error) {
	resPath := t.responsePath(sessionId, reqId)
	_, err := os.Stat(resPath)
	if err == nil {
		return os.ReadFile(resPath)
	}

	ticker := time.NewTicker(t.Config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for response")
		case <-ticker.C:
			_, err := os.Stat(resPath)
			if err != nil {
				continue
			}
			return os.ReadFile(resPath)
		}
	}
}

func (t *FSTransport) SendStream(ctx context.Context, sessionId string, msg *Message) (<-chan *Message, error) {
	return nil, fmt.Errorf("not implemented")
}

func (t *FSTransport) sessionsPath() string {
	return path.Join(t.Config.Root, "sessions")
}

func (t *FSTransport) sessionPath(sessionId string, parts ...string) string {
	ps := []string{t.sessionsPath(), sessionId}
	ps = append(ps, parts...)
	return path.Join(ps...)
}

func (t *FSTransport) requestPath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-req.yaml", reqId))
}

func (t *FSTransport) responsePath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-res.yaml", reqId))
}

func parseRequestIdFromFilename(fn string) (int, error) {
	parts := strings.Split(fn, "-")
	if len(parts) < 2 || parts[1] != "req.yaml" {
		return 0, fmt.Errorf("invalid request filename: %s", fn)
	}

	return strconv.Atoi(parts[0])
}

func parseResponseIdFromFilename(fn string) (int, error) {
	parts := strings.Split(fn, "-")
	if len(parts) < 2 || parts[1] != "res.yaml" {
		return 0, fmt.Errorf("invalid response filename: %s", fn)
	}

	return strconv.Atoi(parts[0])
}
