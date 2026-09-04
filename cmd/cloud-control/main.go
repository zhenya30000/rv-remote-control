package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"
	"github.com/zhenya30000/rv-remote-control/internal/cloud"
	"github.com/zhenya30000/rv-remote-control/internal/config"
	"github.com/zhenya30000/rv-remote-control/internal/httpapi"

	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		slog.Error("cloud control stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadCloud()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	registry := cloud.NewRegistry()
	defer registry.Close()

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	defer grpcListener.Close()

	grpcServer := grpc.NewServer()
	controlpb.RegisterEdgeControlServiceServer(
		grpcServer,
		cloud.NewGRPCServer(registry, cfg.EdgeToken),
	)

	httpServer := httpapi.New(
		cfg.HTTPAddr,
		cfg.ControlAPIToken,
		registry,
		cfg.CommandTimeout,
	)

	serverErr := make(chan error, 2)

	go func() {
		slog.Info("HTTP control API listening", "address", cfg.HTTPAddr)
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	go func() {
		slog.Info("edge gRPC endpoint listening", "address", cfg.GRPCAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-serverErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	_ = httpServer.Shutdown(shutdownCtx)

	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()

	select {
	case <-grpcStopped:
	case <-shutdownCtx.Done():
		grpcServer.Stop()
	}

	return nil
}
