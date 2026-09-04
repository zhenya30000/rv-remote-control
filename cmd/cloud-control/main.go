package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	grpcServer := grpc.NewServer()
	controlpb.RegisterEdgeControlServiceServer(
		grpcServer,
		cloud.NewGRPCServer(registry, cfg.EdgeToken),
	)

	apiServer := httpapi.New(
		cfg.ListenAddr,
		cfg.ControlAPIToken,
		registry,
		cfg.CommandTimeout,
	)

	handler := http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.ProtoMajor == 2 && strings.HasPrefix(
			r.Header.Get("Content-Type"),
			"application/grpc",
		) {
			grpcServer.ServeHTTP(w, r)
			return
		}

		apiServer.Handler().ServeHTTP(w, r)
	})

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         protocols,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info(
			"cloud control listening",
			"address", cfg.ListenAddr,
			"protocols", "http/1.1,h2c",
		)
		if err := server.ListenAndServe();
			err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	_ = server.Shutdown(shutdownCtx)

	return nil
}
