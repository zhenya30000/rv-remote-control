package cloud

import (
	"context"
	"testing"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
)

func TestRegistryDispatchRelay(t *testing.T) {
	registry := NewRegistry()
	session := registry.Register("rv-001")
	defer registry.Unregister("rv-001", session)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	resultCh := make(chan CommandResult, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := registry.DispatchRelay(
			ctx,
			"rv-001",
			2,
			true,
		)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	var command *controlpb.CloudMessage

	select {
	case command = <-session.Outgoing():
	case <-ctx.Done():
		t.Fatal("timed out waiting for command")
	}

	if command.GetChannel() != 2 {
		t.Fatalf("channel = %d, want 2", command.GetChannel())
	}

	if !command.GetEnabled() {
		t.Fatal("enabled = false, want true")
	}

	if !session.Resolve(CommandResult{
		CommandID: command.GetCommandId(),
		Success:   true,
	}) {
		t.Fatal("failed to resolve command")
	}

	select {
	case err := <-errCh:
		t.Fatal(err)
	case result := <-resultCh:
		if !result.Success {
			t.Fatal("result.Success = false, want true")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result")
	}
}

func TestRegistryOffline(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.DispatchRelay(
		context.Background(),
		"missing",
		1,
		true,
	)

	if err != ErrDeviceOffline {
		t.Fatalf("error = %v, want ErrDeviceOffline", err)
	}
}
