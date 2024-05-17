// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/protocol"
	"github.com/gitpod-io/gitpod/genie/pkg/transport"
)

const DEFAULT_REQUEST_TIMEOUT = 5000

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

	requests, err := h.transport.WatchRequests(ctx, h.SessionID)
	if err != nil {
		log.WithError(err).WithField("sessionId", h.SessionID).Error("cannot watch requests")
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-requests:
			log.WithField("requestId", msg.ID).Info("received request")

			req, err := protocol.UnmarshalRequest(msg.Data)
			if err != nil {
				log.WithError(err).WithField("requestId", msg.ID).Error("cannot unmarshal request")
				continue
			}

			go h.handleRequest(ctx, req)
		}
	}
}

func (h *SessionHandler) handleRequest(ctx context.Context, req *protocol.Request) {
	requestTimeout := req.Context.Timeout
	if requestTimeout == 0 || requestTimeout > DEFAULT_REQUEST_TIMEOUT {
		requestTimeout = DEFAULT_REQUEST_TIMEOUT
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(requestTimeout)*time.Millisecond)
	defer cancel()

	log := log.WithField("sessionId", h.SessionID).WithField("requestId", req.ID)
	log.Info("handling request")

	if req.Type != protocol.CallTypeUnary {
		log.Error("unsupported request type")
		return
	}

	// TODO(gpl) Support more :)
	if req.Cmd != "kubectl" {
		log.Error("unsupported command")
		return
	}

	cmd := exec.Command("/usr/bin/kubectl", req.Args...)
	stdoutBuf := bytes.Buffer{}
	cmd.Stdout = &stdoutBuf

	processDone := make(chan *os.ProcessState)
	go func() {
		defer close(processDone)
		err := cmd.Run()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				processDone <- ee.ProcessState
				return
			}
			log.WithError(err).Error("process failed badly")
			return
		}
		processDone <- cmd.ProcessState
	}()

	var ps *os.ProcessState
	select {
	case <-ctx.Done():
		log.Error("request timed out")
		return
	case ps = <-processDone:
	}
	if ps == nil {
		log.Error("process did not finish")
		return
	}
	log.WithField("exitCode", ps.ExitCode()).Info("process finished")

	res := &protocol.Response{
		RequestID:  req.ID,
		SequenceID: 0,
		ExitCode:   ps.ExitCode(),
		Output:     stdoutBuf.String(),
	}
	data, err := res.Marshal()
	if err != nil {
		log.WithError(err).Error("error marshalling response")
		return
	}

	mRes := transport.Message{
		ID:         res.RequestID,
		SequenceID: res.SequenceID,
		Data:       data,
	}
	log.Info("sending response")
	err = h.transport.SendResponse(ctx, h.SessionID, &mRes)
	if err != nil {
		log.WithError(err).Error("error sending response")
		return
	}
	log.Info("response sent")
}
