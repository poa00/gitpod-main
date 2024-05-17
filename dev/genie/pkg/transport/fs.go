// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package transport

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/gitpod-io/gitpod/common-go/log"
)

type FSConfig struct {
	Root string `yaml:"root"`
}

var _ Transport = &FSTransport{}

type FSTransport struct {
	Config *FSConfig

	watchMutex       sync.Mutex
	watcher          *fsnotify.Watcher
	subscribersMutex sync.Mutex
	subscribers      map[string]chan<- *fsnotify.Event
}

func NewFSTransport(cfg *FSConfig) (*FSTransport, error) {
	return &FSTransport{
		Config:      cfg,
		subscribers: map[string]chan<- *fsnotify.Event{},
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
	out := make(chan string)
	sessionsPath := t.sessionsPath()
	err := t.addSubscriber(ctx, func(ev *fsnotify.Event) {
		if !ev.Has(fsnotify.Create) {
			return
		}

		dir, file := path.Split(ev.Name)
		if dir == sessionsPath {
			// directly under "sessions"? then it's a new session
			out <- file
		}
	}, func() {
		close(out)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot add subscriber: %w", err)
	}

	// send initial list of sessions
	entries, err := os.ReadDir(sessionsPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read sessions directory: %w", err)
	}
	go func() {
		for _, entry := range entries {
			if entry.IsDir() {
				out <- entry.Name()
			}
		}
	}()

	return out, err
}

func (t *FSTransport) WatchRequests(ctx context.Context, sessionId string) (<-chan *Message, error) {
	out := make(chan *Message)

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

	err := t.addSubscriber(ctx, func(ev *fsnotify.Event) {
		if !ev.Has(fsnotify.Create) {
			return
		}
		reqID, err := parseRequestIdFromFilename(ev.Name)
		if err != nil {
			return
		}

		// We seem to have a new request here, now read the data
		pushRequest(reqID)
	}, func() {
		close(out)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot add subscriber: %w", err)
	}

	// send initial list of requests (that don't have a response for, yet)
	sessionPath := t.sessionPath(sessionId)
	entries, err := os.ReadDir(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read sessions directory: %w", err)
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

	go func() {
		for reqId, _ := range allRequests {
			log.WithField("requestId", reqId).Info("pushing initial request")
			pushRequest(reqId)
		}
	}()

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
	return fmt.Errorf("not implemented")
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // we only want to listen until we got our response

	resPath := t.responsePath(sessionId, reqId)
	out := make(chan string)
	err := t.addSubscriber(ctx, func(ev *fsnotify.Event) {
		if (ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create)) && ev.Name == resPath {
			out <- resPath // signal that it's there
		}
	}, func() {
		close(out)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot add subscriber: %w", err)
	}

	<-out
	return os.ReadFile(resPath)
}

func (t *FSTransport) SendStream(ctx context.Context, sessionId string, msg *Message) (<-chan *Message, error) {
	return nil, fmt.Errorf("not implemented")
}

func (t *FSTransport) addSubscriber(ctx context.Context, f func(ev *fsnotify.Event), done func()) error {
	_, err := t.ensureWatcher()
	if err != nil {
		return err
	}

	t.subscribersMutex.Lock()
	defer t.subscribersMutex.Unlock()

	k := strconv.Itoa(rand.Int())
	sub := make(chan *fsnotify.Event)
	t.subscribers[k] = sub

	go func() {
		select {
		case ev, more := <-sub:
			if !more {
				// channel was closed by removeSubscribers
				return
			}
			f(ev)
		case <-ctx.Done():
			t.removeSubscribers(k) // also calls close(sub)
		}
		done()
	}()

	return nil
}

func (t *FSTransport) ensureWatcher() (*fsnotify.Watcher, error) {
	t.watchMutex.Lock()
	defer t.watchMutex.Unlock()

	if t.watcher != nil {
		return t.watcher, nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot create watcher: %w", err)
	}

	err = watcher.Add(t.sessionsPath())
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("cannot watch sessions path: %w", err)
	}

	go func() {
		for {
			select {
			case ev, more := <-watcher.Events:
				if !more {
					return
				}
				t.pushToSubscribers(&ev)
			case err, more := <-watcher.Errors:
				if !more {
					return
				}
				log.WithError(err).Error("watcher error")
			}
		}
	}()
	t.watcher = watcher

	return t.watcher, nil
}

func (t *FSTransport) pushToSubscribers(ev *fsnotify.Event) {
	t.subscribersMutex.Lock()

	var toRemove []string
	for k, s := range t.subscribers {
		select {
		case s <- ev:
			// all good
		default:
			// receiver was blocked: mark it for removal
			toRemove = append(toRemove, k)
		}
	}
	t.subscribersMutex.Unlock()

	if len(toRemove) > 0 {
		// remove everybody who was too slow
		t.removeSubscribers(toRemove...)
	}
}

func (t *FSTransport) removeSubscribers(removals ...string) {
	t.subscribersMutex.Lock()
	defer t.subscribersMutex.Unlock()

	for _, k := range removals {
		log.WithField("subscriber", k).Info("removing subscriber")
		sub, ok := t.subscribers[k]
		if !ok {
			continue
		}

		close(sub)
		delete(t.subscribers, k)
	}

	// TODO(gpl): we should also check if we can close the watcher here
}

func (t *FSTransport) sessionsPath() string {
	return path.Join(t.Config.Root, "sessions")
}

func (t *FSTransport) resolvePath(watcherPath string) string {
	return path.Join(t.sessionsPath(), watcherPath)
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
