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
	"sort"
	"strconv"
	"strings"

	"github.com/fsnotify/fsnotify"
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

func (t *FSTransport) SendUnary(ctx context.Context, sessionId string, reqId int, data []byte) ([]byte, error) {
	if !t.HasSession(ctx, sessionId) {
		return nil, fmt.Errorf("session does not exist")
	}

	// write data to file
	reqFileName := t.requestPath(sessionId, reqId)
	err := os.WriteFile(reqFileName, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("error writing request: %w", err)
	}

	// wait for response
	bytes, err := t.waitForResponse(ctx, sessionId, reqId)
	if err != nil {
		return nil, fmt.Errorf("error receiving for response: %w", err)
	}

	return bytes, nil
}

func (t *FSTransport) GetLastRequestID(ctx context.Context, sessionId string) (int, error) {
	if !t.HasSession(ctx, sessionId) {
		return 0, fmt.Errorf("session does not exist")
	}

	entries, err := os.ReadDir(t.sessionPath(sessionId))
	if err != nil {
		return 0, fmt.Errorf("cannot read session directory: %w", err)
	}

	if len(entries) == 0 {
		return 0, nil
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	lastEntry := entries[len(entries)-1]
	reqIDStr, err := parseRequestIdFromFilename(lastEntry.Name())
	if err != nil {
		return 0, fmt.Errorf("error reading last request: %w", err)
	}
	reqID, err := strconv.Atoi(reqIDStr)
	if err != nil {
		return 0, fmt.Errorf("error reading last request: %w", err)
	}
	return reqID, nil
}

func (t *FSTransport) waitForResponse(ctx context.Context, sessionId string, reqId int) ([]byte, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot create watcher: %w", err)
	}
	defer watcher.Close()

	err = watcher.Add(t.sessionPath(sessionId))
	if err != nil {
		return nil, fmt.Errorf("cannot watch session path: %w", err)
	}

	resPath := t.responsePath(sessionId, reqId)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ev := <-watcher.Events:
			if (ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create)) && ev.Name == resPath {
				return os.ReadFile(resPath)
			}
		case err := <-watcher.Errors:
			return nil, fmt.Errorf("watcher error: %w", err)
		}
	}
}

func (t *FSTransport) SendStream(ctx context.Context, sessionId string, id int, data []byte) (<-chan []byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (t *FSTransport) sessionPath(sessionId string, parts ...string) string {
	ps := []string{t.Config.Root, "sessions", sessionId}
	ps = append(ps, parts...)
	return path.Join(ps...)
}

func (t *FSTransport) requestPath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-res.yaml", reqId))
}

func (t *FSTransport) responsePath(sessionId string, reqId int) string {
	return t.sessionPath(sessionId, fmt.Sprintf("%d-req.yaml", reqId))
}

func parseRequestIdFromFilename(fn string) (string, error) {
	parts := strings.Split(fn, "-")
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid request filename: %s", fn)
	}
	return parts[0], nil
}
