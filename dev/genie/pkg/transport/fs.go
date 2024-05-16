// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"context"
	"fmt"
)

type FSConfig struct {
	Root string `yaml:"root"`
}

var _ Transport = &FSTransport{}

type FSTransport struct {
	Config *FSConfig
}

func NewFSTransport(cfg *FSConfig) (*FSTransport, error) {
	return &FSTransport{
		Config: cfg,
	}, nil
}

func (t *FSTransport) CreateSession(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented")
}

func (t *FSTransport) HasSession(ctx context.Context, id string) bool {
	return false
}

func (t *FSTransport) SendUnary(ctx context.Context, sessionId, id string, data []byte) ([]byte, error) {
	return []byte{}, fmt.Errorf("not implemented")
}

func (t *FSTransport) SendStream(ctx context.Context, sessionId, id string, data []byte) (<-chan []byte, error) {
	return nil, fmt.Errorf("not implemented")
}
