package edge

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
	"github.com/zhenya30000/rv-remote-control/internal/gateway"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Config struct {
	DeviceID          string
	Token             string
	CloudAddress      string
	CloudInsecure     bool
	CloudServerName   string
	CommandTimeout    time.Duration
	HeartbeatInterval time.Duration
}

type Agent struct {
	cfg   Config
	relay gateway.Relay
}

func New(
	cfg Config,
	relay gateway.Relay,
) *Agent {
	return &Agent{
		cfg:   cfg,
		relay: relay,
	}
}

func (a *Agent) RunConnection(ctx context.Context) error {
	transportCredentials := credentials.TransportCredentials(
		credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: a.cfg.CloudServerName,
		}),
	)

	if a.cfg.CloudInsecure {
		transportCredentials = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(
		a.cfg.CloudAddress,
		grpc.WithTransportCredentials(transportCredentials),
	)
	if err != nil {
		return fmt.Errorf("create cloud gRPC client: %w", err)
	}
	defer conn.Close()

	client := controlpb.NewEdgeControlServiceClient(conn)

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sessionCtx = metadata.AppendToOutgoingContext(
		sessionCtx,
		"x-device-id", a.cfg.DeviceID,
		"authorization", "Bearer "+a.cfg.Token,
	)

	stream, err := client.Connect(sessionCtx)
	if err != nil {
		return fmt.Errorf("open cloud control stream: %w", err)
	}

	outgoing := make(chan *controlpb.EdgeMessage, 16)
	sendErr := make(chan error, 1)

	go func() {
		sendErr <- sendLoop(sessionCtx, stream, outgoing)
		cancel()
	}()

	go heartbeatLoop(
		sessionCtx,
		outgoing,
		a.cfg.HeartbeatInterval,
	)

	slog.Info(
		"cloud control stream connected",
		"device_id", a.cfg.DeviceID,
		"cloud", a.cfg.CloudAddress,
	)

	for {
		message, err := stream.Recv()
		if err != nil {
			select {
			case sendFailure := <-sendErr:
				if sendFailure != nil {
					return fmt.Errorf("cloud stream send: %w", sendFailure)
				}
			default:
			}

			return fmt.Errorf("cloud stream receive: %w", err)
		}

		if message.GetKind() != controlpb.CloudMessageKindRelayCommand {
			slog.Warn(
				"unknown cloud message",
				"kind", message.GetKind(),
			)
			continue
		}

		result := a.executeRelayCommand(
			sessionCtx,
			message,
		)

		select {
		case outgoing <- result:
		case <-sessionCtx.Done():
			return sessionCtx.Err()
		}
	}
}

func (a *Agent) executeRelayCommand(
	ctx context.Context,
	message *controlpb.CloudMessage,
) *controlpb.EdgeMessage {
	commandCtx, cancel := context.WithTimeout(
		ctx,
		a.cfg.CommandTimeout,
	)
	defer cancel()

	err := a.relay.SetChannel(
		commandCtx,
		message.GetChannel(),
		message.GetEnabled(),
	)

	result := &controlpb.EdgeMessage{
		Kind:       controlpb.EdgeMessageKindCommandResult,
		CommandId:  message.GetCommandId(),
		Success:    err == nil,
		UnixMillis: time.Now().UnixMilli(),
	}

	if err != nil {
		result.Error = err.Error()
		slog.Error(
			"relay command failed",
			"command_id", message.GetCommandId(),
			"channel", message.GetChannel(),
			"enabled", message.GetEnabled(),
			"error", err,
		)
		return result
	}

	slog.Info(
		"relay command applied",
		"command_id", message.GetCommandId(),
		"channel", message.GetChannel(),
		"enabled", message.GetEnabled(),
	)

	return result
}

func sendLoop(
	ctx context.Context,
	stream controlpb.EdgeControlService_ConnectClient,
	outgoing <-chan *controlpb.EdgeMessage,
) error {
	for {
		select {
		case message := <-outgoing:
			if err := stream.Send(message); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func heartbeatLoop(
	ctx context.Context,
	outgoing chan<- *controlpb.EdgeMessage,
	interval time.Duration,
) {
	send := func() bool {
		select {
		case outgoing <- &controlpb.EdgeMessage{
			Kind:       controlpb.EdgeMessageKindHeartbeat,
			UnixMillis: time.Now().UnixMilli(),
		}:
			return true
		case <-ctx.Done():
			return false
		}
	}

	if !send() {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !send() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
