package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhenya30000/rv-remote-control/internal/cloud"
)

type fakeDispatcher struct {
	result cloud.CommandResult
	err    error

	deviceID string
	channel  uint32
	enabled  bool
}

func (f *fakeDispatcher) DispatchRelay(
	_ context.Context,
	deviceID string,
	channel uint32,
	enabled bool,
) (cloud.CommandResult, error) {
	f.deviceID = deviceID
	f.channel = channel
	f.enabled = enabled
	return f.result, f.err
}

func (f *fakeDispatcher) Status(
	deviceID string,
) (cloud.DeviceStatus, bool) {
	return cloud.DeviceStatus{
		DeviceID: deviceID,
		Online:   true,
	}, true
}

func TestHealth(t *testing.T) {
	server := New(
		":0",
		"secret",
		&fakeDispatcher{},
		time.Second,
	)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code,
			http.StatusOK,
			response.Body.String(),
		)
	}

	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestRelayCommand(t *testing.T) {
	dispatcher := &fakeDispatcher{
		result: cloud.CommandResult{
			CommandID: "abc",
			Success:   true,
		},
	}

	server := New(
		":0",
		"secret",
		dispatcher,
		time.Second,
	)

	request := httptest.NewRequest(
		http.MethodPut,
		"/v1/devices/rv-001/relays/3",
		strings.NewReader("{\"enabled\":true}"),
	)
	request.Header.Set("Authorization", "Bearer secret")

	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d; body=%s",
			response.Code,
			http.StatusOK,
			response.Body.String(),
		)
	}

	if dispatcher.deviceID != "rv-001" ||
		dispatcher.channel != 3 ||
		!dispatcher.enabled {
		t.Fatalf(
			"dispatch = (%s, %d, %t)",
			dispatcher.deviceID,
			dispatcher.channel,
			dispatcher.enabled,
		)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body["status"] != "applied" {
		t.Fatalf("status body = %v, want applied", body["status"])
	}
}

func TestRelayCommandRequiresAuth(t *testing.T) {
	server := New(
		":0",
		"secret",
		&fakeDispatcher{},
		time.Second,
	)

	request := httptest.NewRequest(
		http.MethodPut,
		"/v1/devices/rv-001/relays/1",
		strings.NewReader("{\"enabled\":true}"),
	)

	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusUnauthorized,
		)
	}
}
