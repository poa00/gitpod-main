// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/protocol"
	"github.com/gitpod-io/gitpod/genie/pkg/transport"
)

type Config struct {
	Transport transport.TransportConfig `yaml:"transport"`
	Handler   HandlerConfig             `yaml:"handler"`
}

type HandlerConfig struct {
	Binaries map[string]string                   `yaml:"binaries"`
	Timeouts map[protocol.CallType]time.Duration `yaml:"timeouts"`
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
		case newSession, more := <-watcher:
			if !more {
				log.Info("watcher closed")
				break loop
			}
			g.addSessionHandlerIfNew(ctx, newSession, t)
		}
	}
	log.Info("stopped watching for new sessions")
	cancel()

	log.Info("stopped session handlers")
	return ctx.Err()
}

func (g *GenieServer) addSessionHandlerIfNew(ctx context.Context, newSessionId string, t transport.Transport) {
	g.sessionsMutex.Lock()
	defer g.sessionsMutex.Unlock()

	if g.sessions[newSessionId] != nil {
		return
	}
	h := NewSessionHandler(newSessionId, t, &g.Config.Handler, func() {
		g.removeSessionHandler(newSessionId)
	})
	g.sessions[newSessionId] = h
	go h.Run(ctx)
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
	config    *HandlerConfig
	quitFn    func()
}

func NewSessionHandler(sessionID string, t transport.Transport, config *HandlerConfig, quitFn func()) *SessionHandler {
	return &SessionHandler{
		SessionID: sessionID,
		transport: t,
		config:    config,
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
		log.WithError(err).Error("cannot watch requests")
		return
	}
	log.Info("watching for new requests")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, more := <-requests:
			if !more {
				log.Info("requests channel closed")
				return
			}
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
	requestTimeout := time.Duration(req.Context.Timeout) * time.Millisecond
	defaultTimeout := h.config.Timeouts[req.Type]
	if requestTimeout == 0 || requestTimeout.Milliseconds() > defaultTimeout.Milliseconds() {
		requestTimeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	log := log.WithField("sessionId", h.SessionID).WithField("requestId", req.ID)
	log.Info("handling request")

	sendErrResponse := func(errMsg string) {
		log.Error(errMsg)
		res := &protocol.Response{
			RequestID:  req.ID,
			SequenceID: 0,
			ExitCode:   -1,
			Output:     fmt.Sprintf("error: %s", errMsg),
		}
		err := h.sendResponse(ctx, res)
		if err != nil {
			log.WithError(err).Error("error sending error response")
		}
	}

	if req.Type != protocol.CallTypeUnary {
		log.Error("unsupported request type")
		return
	}

	// TODO(gpl) Support more :)
	if req.Cmd != "kubectl" {
		sendErrResponse("unsupported command")
		return
	}

	if len(req.Args) < 1 {
		sendErrResponse("auth: invalid args")
		return
	}

	if req.Args[0] != "get" && req.Args[0] != "describe" {
		sendErrResponse("auth: command not allowed")
		return
	}

	binary, ok := h.config.Binaries[req.Cmd]
	if !ok {
		sendErrResponse("no binary configured for cmd: " + req.Cmd)
		return
	}

	cmd := exec.Command(binary, req.Args...)
	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(&stdBuffer)
	cmd.Stdout = mw
	cmd.Stderr = mw

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
		Output:     stdBuffer.String(),
	}
	err := h.sendResponse(ctx, res)
	if err != nil {
		log.WithError(err).Error("error sending response")
		return
	}
}

func (h *SessionHandler) sendResponse(ctx context.Context, res *protocol.Response) error {
	log := log.WithField("requestId", res.RequestID)
	log.Info("sending response")

	data, err := res.Marshal()
	if err != nil {
		return fmt.Errorf("error marshalling response: %w", err)
	}

	mRes := transport.Message{
		ID:         res.RequestID,
		SequenceID: res.SequenceID,
		Data:       data,
	}
	err = h.transport.SendResponse(ctx, h.SessionID, &mRes)
	if err != nil {
		return err
	}

	log.Info("response sent")
	return nil
}
