// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package server

import (
	"context"
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v2"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/transport"
)

type Config struct {
	Transport transport.TransportConfig `yaml:"transport"`
}

type GenieServer struct {
	Config *Config

	sessionsMutex sync.Mutex
	sessions      map[string]*SessionHandler
}

func NewGenieServer(cfg *Config) *GenieServer {
	return &GenieServer{
		Config: cfg,

		sessionsMutex: sync.Mutex{},
		sessions:      map[string]*SessionHandler{},
	}
}

func (g *GenieServer) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)

	t, err := transport.NewTransport(&g.Config.Transport)
	if err != nil {
		cancel()
		return fmt.Errorf("cannot create transport: %w", err)
	}
	log.Info("transport created")

	// Listen to new sessions, and start a new session handler for each
	watcher, err := t.WatchSessions(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("cannot watch sessions: %w", err)
	}
	log.Info("watching for new sessions")

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case newSession := <-watcher:
			if g.sessions[newSession] == nil {
				h := NewSessionHandler(t, newSession, func() {
					g.removeSessionHandler(newSession)
				})
				g.addSessionHandler(newSession, h)
				go h.Run(ctx)
			}
		}
	}
	log.Info("stopped watching for new sessions")
	cancel()

	log.Info("stopped session handlers")
	return ctx.Err()
}

func (g *GenieServer) addSessionHandler(sessionID string, h *SessionHandler) {
	g.sessionsMutex.Lock()
	defer g.sessionsMutex.Unlock()

	g.sessions[sessionID] = h
}

func (g *GenieServer) removeSessionHandler(sessionID string) {
	g.sessionsMutex.Lock()
	defer g.sessionsMutex.Unlock()

	delete(g.sessions, sessionID)
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

type SessionHandler struct {
	SessionID string
	transport transport.Transport
	quitFn    func()
}

func NewSessionHandler(t transport.Transport, sessionID string, quitFn func()) *SessionHandler {
	return &SessionHandler{
		SessionID: sessionID,
		transport: t,
		quitFn:    quitFn,
	}
}

func (h *SessionHandler) Run(ctx context.Context) {
	defer h.quitFn()
	log := log.WithField("sessionId", h.SessionID)
	log.Info("session started")
	defer log.Info("session stopped")

}
