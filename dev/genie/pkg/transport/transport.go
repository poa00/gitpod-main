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
	GetLastRequestID(ctx context.Context, sessionId string) (int, error)

	SendUnary(ctx context.Context, sessionId string, id int, data []byte) ([]byte, error)
	SendStream(ctx context.Context, sessionId string, id int, data []byte) (<-chan []byte, error)
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
