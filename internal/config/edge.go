package config

import (
	"fmt"
	"os"
	"time"
)

type Edge struct {
	DeviceID          string
	EdgeToken         string
	CloudGRPCAddr     string
	CloudInsecure     bool
	CloudServerName   string
	BLEGatewayAddr    string
	CommandTimeout    time.Duration
	HeartbeatInterval time.Duration
}

func LoadEdge() (Edge, error) {
	insecure, err := boolEnv("CLOUD_INSECURE", false)
	if err != nil {
		return Edge{}, err
	}

	commandTimeout, err := durationEnv("EDGE_COMMAND_TIMEOUT", 4*time.Second)
	if err != nil {
		return Edge{}, err
	}

	heartbeatInterval, err := durationEnv("HEARTBEAT_INTERVAL", 15*time.Second)
	if err != nil {
		return Edge{}, err
	}

	cfg := Edge{
		DeviceID:          os.Getenv("DEVICE_ID"),
		EdgeToken:         os.Getenv("EDGE_TOKEN"),
		CloudGRPCAddr:     os.Getenv("CLOUD_GRPC_ADDR"),
		CloudInsecure:     insecure,
		CloudServerName:   os.Getenv("CLOUD_SERVER_NAME"),
		BLEGatewayAddr:    env("BLE_GATEWAY_ADDR", "127.0.0.1:50051"),
		CommandTimeout:    commandTimeout,
		HeartbeatInterval: heartbeatInterval,
	}

	switch {
	case cfg.DeviceID == "":
		return Edge{}, fmt.Errorf("DEVICE_ID is required")
	case cfg.EdgeToken == "":
		return Edge{}, fmt.Errorf("EDGE_TOKEN is required")
	case cfg.CloudGRPCAddr == "":
		return Edge{}, fmt.Errorf("CLOUD_GRPC_ADDR is required")
	}

	return cfg, nil
}
