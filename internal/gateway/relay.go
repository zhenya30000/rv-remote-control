package gateway

import (
	"context"
	"fmt"
	"strings"

	relaypb "github.com/zhenya30000/ble-device-gateway/gen/relay/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Relay interface {
	SetChannel(
		ctx context.Context,
		channel uint32,
		enabled bool,
	) error
}

type RelayClient struct {
	conn   *grpc.ClientConn
	client relaypb.RelayServiceClient
}

func NewRelayClient(address string) (*RelayClient, error) {
	target := directTarget(address)

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create BLE gateway client: %w", err)
	}

	return &RelayClient{
		conn:   conn,
		client: relaypb.NewRelayServiceClient(conn),
	}, nil
}

func directTarget(address string) string {
	if strings.Contains(address, "://") {
		return address
	}

	return "passthrough:///" + address
}

func (c *RelayClient) SetChannel(
	ctx context.Context,
	channel uint32,
	enabled bool,
) error {
	response, err := c.client.SetChannel(
		ctx,
		&relaypb.SetChannelRequest{
			Channel: channel,
			Enabled: enabled,
		},
	)
	if err != nil {
		return fmt.Errorf("BLE gateway SetChannel: %w", err)
	}

	if response.GetChannel() != channel ||
		response.GetEnabled() != enabled {
		return fmt.Errorf(
			"BLE gateway returned unexpected state: channel=%d enabled=%t",
			response.GetChannel(),
			response.GetEnabled(),
		)
	}

	return nil
}

func (c *RelayClient) Close() error {
	return c.conn.Close()
}
