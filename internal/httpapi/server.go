package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/zhenya30000/rv-remote-control/internal/auth"
	"github.com/zhenya30000/rv-remote-control/internal/cloud"
)

type RelayDispatcher interface {
	DispatchRelay(
		ctx context.Context,
		deviceID string,
		channel uint32,
		enabled bool,
	) (cloud.CommandResult, error)

	Status(deviceID string) (cloud.DeviceStatus, bool)
}

type Server struct {
	httpServer     *http.Server
	apiToken       string
	dispatcher     RelayDispatcher
	commandTimeout time.Duration
}

func New(
	addr string,
	apiToken string,
	dispatcher RelayDispatcher,
	commandTimeout time.Duration,
) *Server {
	server := &Server{
		apiToken:       apiToken,
		dispatcher:     dispatcher,
		commandTimeout: commandTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.handleHealth)
	mux.HandleFunc(
		"GET /v1/devices/{deviceID}/status",
		server.authorize(server.handleStatus),
	)
	mux.HandleFunc(
		"PUT /v1/devices/{deviceID}/relays/{channel}",
		server.authorize(server.handleRelay),
	)

	server.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(
	w http.ResponseWriter,
	_ *http.Request,
) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleStatus(
	w http.ResponseWriter,
	r *http.Request,
) {
	deviceID := r.PathValue("deviceID")
	statusValue, ok := s.dispatcher.Status(deviceID)

	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"device_id": deviceID,
			"online":    false,
		})
		return
	}

	writeJSON(w, http.StatusOK, statusValue)
}

func (s *Server) handleRelay(
	w http.ResponseWriter,
	r *http.Request,
) {
	deviceID := r.PathValue("deviceID")

	channelValue, err := strconv.ParseUint(
		r.PathValue("channel"),
		10,
		32,
	)
	if err != nil || channelValue < 1 || channelValue > 4 {
		writeError(
			w,
			http.StatusBadRequest,
			"relay channel must be between 1 and 4",
		)
		return
	}

	var request struct {
		Enabled *bool `json:"enabled"`
	}

	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		s.commandTimeout,
	)
	defer cancel()

	result, err := s.dispatcher.DispatchRelay(
		ctx,
		deviceID,
		uint32(channelValue),
		*request.Enabled,
	)
	if err != nil {
		switch {
		case errors.Is(err, cloud.ErrDeviceOffline):
			writeError(w, http.StatusServiceUnavailable, "device is offline")
		case errors.Is(err, cloud.ErrSessionBusy):
			writeError(w, http.StatusServiceUnavailable, "device command queue is busy")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "device command timed out")
		case errors.Is(err, context.Canceled):
			writeError(w, http.StatusRequestTimeout, "request canceled")
		default:
			writeError(w, http.StatusInternalServerError, "command dispatch failed")
		}
		return
	}

	if !result.Success {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"command_id": result.CommandID,
			"status":     "failed",
			"error":      result.Error,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"command_id": result.CommandID,
		"status":     "applied",
		"device_id":  deviceID,
		"channel":    channelValue,
		"enabled":    *request.Enabled,
	})
}

func (s *Server) authorize(
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.BearerMatches(
			r.Header.Get("Authorization"),
			s.apiToken,
		) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		next(w, r)
	}
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()

	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON object")
	}

	return nil
}

func writeError(
	w http.ResponseWriter,
	statusCode int,
	message string,
) {
	writeJSON(w, statusCode, map[string]string{
		"error": message,
	})
}

func writeJSON(
	w http.ResponseWriter,
	statusCode int,
	value any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
