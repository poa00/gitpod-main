// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"context"
	"fmt"
)

type Transport interface {
	CreateSession(ctx context.Context, sessionId string) error
	HasSession(ctx context.Context, sessionId string) bool
	WatchSessions(ctx context.Context) (<-chan string, error)
	GetLastRequestID(ctx context.Context, sessionId string) (int, error)
	WatchRequests(ctx context.Context, sessionId string) (<-chan *Message, error)

	SendUnary(ctx context.Context, sessionId string, msg *Message) (*Message, error)
	SendStream(ctx context.Context, sessionId string, msg *Message) (<-chan *Message, error)
	SendResponse(ctx context.Context, sessionId string, msg *Message) error
}

type Message struct {
	ID         int
	SequenceID int
	Data       []byte
}

type TransportConfig struct {
	FSConfig *FSConfig `yaml:"fs,omitempty"`
	S3Config *S3Config `yaml:"s3,omitempty"`
}

func NewTransport(cfg *TransportConfig) (Transport, error) {
	if cfg.FSConfig != nil {
		return NewFSTransport(cfg.FSConfig)
	}
	if cfg.S3Config != nil {
		return NewS3Transport(cfg.S3Config)
	}
	return nil, fmt.Errorf("no transport configuration found")
}
