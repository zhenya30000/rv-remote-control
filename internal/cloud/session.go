package cloud

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
)

var (
	ErrDeviceOffline = errors.New("device is offline")
	ErrSessionBusy   = errors.New("device command queue is full")
)

type CommandResult struct {
	CommandID string
	Success   bool
	Error     string
}

type Session struct {
	deviceID    string
	connectedAt time.Time
	send        chan *controlpb.CloudMessage
	done        chan struct{}

	lastSeen atomic.Int64

	mu      sync.Mutex
	pending map[string]chan CommandResult
	closed  bool
}

func newSession(deviceID string) *Session {
	now := time.Now()

	s := &Session{
		deviceID:    deviceID,
		connectedAt: now,
		send:        make(chan *controlpb.CloudMessage, 32),
		done:        make(chan struct{}),
		pending:     make(map[string]chan CommandResult),
	}
	s.lastSeen.Store(now.UnixMilli())

	return s
}

func (s *Session) DeviceID() string {
	return s.deviceID
}

func (s *Session) Outgoing() <-chan *controlpb.CloudMessage {
	return s.send
}

func (s *Session) Done() <-chan struct{} {
	return s.done
}

func (s *Session) MarkSeen(at time.Time) {
	s.lastSeen.Store(at.UnixMilli())
}

func (s *Session) Status() DeviceStatus {
	return DeviceStatus{
		DeviceID:    s.deviceID,
		Online:      true,
		ConnectedAt: s.connectedAt,
		LastSeen:    time.UnixMilli(s.lastSeen.Load()),
	}
}

func (s *Session) Dispatch(
	ctx context.Context,
	message *controlpb.CloudMessage,
) (CommandResult, error) {
	resultCh := make(chan CommandResult, 1)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return CommandResult{}, ErrDeviceOffline
	}
	s.pending[message.GetCommandId()] = resultCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, message.GetCommandId())
		s.mu.Unlock()
	}()

	select {
	case s.send <- message:
	case <-s.done:
		return CommandResult{}, ErrDeviceOffline
	case <-ctx.Done():
		return CommandResult{}, ctx.Err()
	default:
		return CommandResult{}, ErrSessionBusy
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-s.done:
		return CommandResult{}, ErrDeviceOffline
	case <-ctx.Done():
		return CommandResult{}, ctx.Err()
	}
}

func (s *Session) Resolve(result CommandResult) bool {
	s.mu.Lock()
	resultCh, ok := s.pending[result.CommandID]
	s.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case resultCh <- result:
		return true
	default:
		return false
	}
}

func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
}
