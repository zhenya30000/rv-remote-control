package cloud

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
)

type DeviceStatus struct {
	DeviceID    string    `json:"device_id"`
	Online      bool      `json:"online"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeen    time.Time `json:"last_seen"`
}

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[string]*Session),
	}
}

func (r *Registry) Register(deviceID string) *Session {
	session := newSession(deviceID)

	r.mu.Lock()
	previous := r.sessions[deviceID]
	r.sessions[deviceID] = session
	r.mu.Unlock()

	if previous != nil {
		previous.Close()
	}

	return session
}

func (r *Registry) Unregister(deviceID string, session *Session) {
	r.mu.Lock()
	if r.sessions[deviceID] == session {
		delete(r.sessions, deviceID)
	}
	r.mu.Unlock()

	session.Close()
}

func (r *Registry) Status(deviceID string) (DeviceStatus, bool) {
	r.mu.RLock()
	session := r.sessions[deviceID]
	r.mu.RUnlock()

	if session == nil {
		return DeviceStatus{
			DeviceID: deviceID,
			Online:   false,
		}, false
	}

	return session.Status(), true
}

func (r *Registry) DispatchRelay(
	ctx context.Context,
	deviceID string,
	channel uint32,
	enabled bool,
) (CommandResult, error) {
	r.mu.RLock()
	session := r.sessions[deviceID]
	r.mu.RUnlock()

	if session == nil {
		return CommandResult{}, ErrDeviceOffline
	}

	commandID, err := newCommandID()
	if err != nil {
		return CommandResult{}, err
	}

	return session.Dispatch(
		ctx,
		&controlpb.CloudMessage{
			Kind:       controlpb.CloudMessageKindRelayCommand,
			CommandId:  commandID,
			Channel:    channel,
			Enabled:    enabled,
			UnixMillis: time.Now().UnixMilli(),
		},
	)
}

func (r *Registry) Close() {
	r.mu.Lock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.sessions = make(map[string]*Session)
	r.mu.Unlock()

	for _, session := range sessions {
		session.Close()
	}
}

func newCommandID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(value[:]), nil
}
