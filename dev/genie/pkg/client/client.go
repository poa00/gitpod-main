// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package client

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/gitpod-io/gitpod/genie/pkg/protocol"
	"github.com/gitpod-io/gitpod/genie/pkg/transport"
)

type Config struct {
	Transport      transport.TransportConfig `yaml:"transport"`
	CurrentSession string                    `yaml:"current_session,omitempty"`
}

type Client struct {
	Config    *Config
	Transport transport.Transport
}

func NewClient(cfg *Config) (*Client, error) {
	t, err := transport.NewTransport(&cfg.Transport)
	if err != nil {
		return nil, fmt.Errorf("cannot create transport: %w", err)
	}

	return &Client{
		Config:    cfg,
		Transport: t,
	}, nil
}

func (c *Client) CreateSession(ctx context.Context, name string) (string, error) {
	sessionName := fmt.Sprintf("%s-%s", time.Now().Format("2006_01_02_15_04"), name)
	err := c.Transport.CreateSession(ctx, sessionName)
	if err != nil {
		return "", fmt.Errorf("cannot create session: %w", err)
	}
	return sessionName, nil
}

func (c *Client) EnsureSession(ctx context.Context) (string, error) {
	currentSessionID := os.Getenv("GENIE_SESSION")
	if !c.Transport.HasSession(ctx, currentSessionID) {
		return currentSessionID, fmt.Errorf("current session does not exist")
	}
	return currentSessionID, nil
}

func (c *Client) Send(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if req.Type == protocol.CallTypeStream {
		return nil, fmt.Errorf("streaming requests are not supported yet")
	}

	reqID, err := c.Transport.GetLastRequestID(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("error preparing request: %w", err)
	}
	req.ID = reqID + 1

	data, err := req.Marshal()
	if err != nil {
		return nil, fmt.Errorf("error serializing request: %w", err)
	}

	mReq := transport.Message{
		ID:   req.ID,
		Data: data,
	}
	mRes, err := c.Transport.SendUnary(ctx, req.SessionID, &mReq)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}

	res, err := protocol.UnmarshalResponse(mRes.Data)
	if err != nil {
		return nil, fmt.Errorf("error deserializing response: %w", err)
	}

	return res, nil
}

func LoadClient(configPathArg string) (*Client, error) {
	configPath := configPathArg
	if configPath == "" {
		configPath = os.Getenv("GENIE_CONFIG")
	}
	if configPath == "" {
		return nil, fmt.Errorf("config file path is required but not provided")
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return NewClient(config)
}

func LoadConfig(path string) (*Config, error) {
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	c := &Config{}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		return nil, fmt.Errorf("Error parsing config file: %v", err)
	}

	return c, nil
}
