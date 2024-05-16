// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"context"
	"fmt"
)

type S3Config struct {
}

var _ Transport = &FSTransport{}

type S3Transport struct {
	Config *S3Config
}

func NewS3Transport(cfg *S3Config) (*S3Transport, error) {
	return &S3Transport{
		Config: cfg,
	}, nil
}

func (t *S3Transport) CreateSession(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented")
}

func (t *S3Transport) HasSession(ctx context.Context, id string) bool {
	return false
}

func (t *S3Transport) SendUnary(ctx context.Context, sessionId, id string, data []byte) ([]byte, error) {
	return []byte{}, fmt.Errorf("not implemented")
}

func (t *S3Transport) SendStream(ctx context.Context, sessionId, id string, data []byte) (<-chan []byte, error) {
	return nil, fmt.Errorf("not implemented")
}
