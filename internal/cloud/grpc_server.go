package cloud

import (
	"crypto/subtle"
	"io"
	"log/slog"
	"strings"
	"time"

	controlpb "github.com/zhenya30000/rv-remote-control/gen/control/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const deviceIDMetadataKey = "x-device-id"

type GRPCServer struct {
	controlpb.UnimplementedEdgeControlServiceServer

	registry  *Registry
	edgeToken string
}

func NewGRPCServer(
	registry *Registry,
	edgeToken string,
) *GRPCServer {
	return &GRPCServer{
		registry:  registry,
		edgeToken: edgeToken,
	}
}

func (s *GRPCServer) Connect(
	stream controlpb.EdgeControlService_ConnectServer,
) error {
	deviceID, err := s.authenticate(stream)
	if err != nil {
		return err
	}

	session := s.registry.Register(deviceID)
	defer s.registry.Unregister(deviceID, session)

	slog.Info("edge connected", "device_id", deviceID)

	recvErr := make(chan error, 1)
	go s.receiveLoop(stream, session, recvErr)

	for {
		select {
		case message := <-session.Outgoing():
			if err := stream.Send(message); err != nil {
				return err
			}

		case err := <-recvErr:
			if err == io.EOF {
				return nil
			}
			return err

		case <-session.Done():
			return status.Error(
				codes.Aborted,
				"device session replaced or closed",
			)

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

func (s *GRPCServer) receiveLoop(
	stream controlpb.EdgeControlService_ConnectServer,
	session *Session,
	recvErr chan<- error,
) {
	for {
		message, err := stream.Recv()
		if err != nil {
			recvErr <- err
			return
		}

		session.MarkSeen(time.Now())

		switch message.GetKind() {
		case controlpb.EdgeMessageKindHeartbeat:
			continue

		case controlpb.EdgeMessageKindCommandResult:
			session.Resolve(CommandResult{
				CommandID: message.GetCommandId(),
				Success:   message.GetSuccess(),
				Error:     message.GetError(),
			})

		default:
			slog.Warn(
				"unknown edge message",
				"device_id", session.DeviceID(),
				"kind", message.GetKind(),
			)
		}
	}
}

func (s *GRPCServer) authenticate(
	stream controlpb.EdgeControlService_ConnectServer,
) (string, error) {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	deviceID := first(md.Get(deviceIDMetadataKey))
	if deviceID == "" {
		return "", status.Error(codes.Unauthenticated, "missing device id")
	}

	authorization := first(md.Get("authorization"))
	const prefix = "Bearer "

	if !strings.HasPrefix(authorization, prefix) {
		return "", status.Error(codes.Unauthenticated, "missing bearer token")
	}

	provided := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	if len(provided) != len(s.edgeToken) ||
		subtle.ConstantTimeCompare([]byte(provided), []byte(s.edgeToken)) != 1 {
		return "", status.Error(codes.Unauthenticated, "invalid edge token")
	}

	return deviceID, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
