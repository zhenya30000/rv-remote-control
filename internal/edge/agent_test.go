package edge

import (
	"context"
	"errors"
	"testing"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
)

type fakeRelay struct {
	channel uint32
	enabled bool
	err     error
}

func (f *fakeRelay) SetChannel(
	_ context.Context,
	channel uint32,
	enabled bool,
) error {
	f.channel = channel
	f.enabled = enabled
	return f.err
}

func TestExecuteRelayCommand(t *testing.T) {
	relay := &fakeRelay{}
	agent := New(
		Config{
			CommandTimeout: time.Second,
		},
		relay,
	)

	result := agent.executeRelayCommand(
		context.Background(),
		&controlpb.CloudMessage{
			Kind:      controlpb.CloudMessageKindRelayCommand,
			CommandId: "cmd-1",
			Channel:   4,
			Enabled:   true,
		},
	)

	if !result.GetSuccess() {
		t.Fatalf("success = false, error=%s", result.GetError())
	}

	if relay.channel != 4 || !relay.enabled {
		t.Fatalf(
			"relay call = (%d, %t), want (4, true)",
			relay.channel,
			relay.enabled,
		)
	}
}

func TestExecuteRelayCommandReportsFailure(t *testing.T) {
	relay := &fakeRelay{
		err: errors.New("relay unavailable"),
	}

	agent := New(
		Config{
			CommandTimeout: time.Second,
		},
		relay,
	)

	result := agent.executeRelayCommand(
		context.Background(),
		&controlpb.CloudMessage{
			CommandId: "cmd-2",
			Channel:   1,
		},
	)

	if result.GetSuccess() {
		t.Fatal("success = true, want false")
	}

	if result.GetError() == "" {
		t.Fatal("expected error text")
	}
}
