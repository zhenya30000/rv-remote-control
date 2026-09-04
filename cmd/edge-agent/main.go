package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhenya30000/rv-remote-control/internal/config"
	"github.com/zhenya30000/rv-remote-control/internal/edge"
	"github.com/zhenya30000/rv-remote-control/internal/gateway"
	"github.com/zhenya30000/rv-remote-control/internal/retry"
)

func main() {
	cfg, err := config.LoadEdge()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	relay, err := gateway.NewRelayClient(cfg.BLEGatewayAddr)
	if err != nil {
		slog.Error("create BLE gateway client", "error", err)
		os.Exit(1)
	}
	defer relay.Close()

	agent := edge.New(
		edge.Config{
			DeviceID:          cfg.DeviceID,
			Token:             cfg.EdgeToken,
			CloudAddress:      cfg.CloudGRPCAddr,
			CloudInsecure:     cfg.CloudInsecure,
			CloudServerName:   cfg.CloudServerName,
			CommandTimeout:    cfg.CommandTimeout,
			HeartbeatInterval: cfg.HeartbeatInterval,
		},
		relay,
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	retry.Run(
		ctx,
		"cloud control connection",
		agent.RunConnection,
	)
}
