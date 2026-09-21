// Package grpcapi implements guardian.v1.ConnectorService — the internal
// boundary downstream platform services consume. It translates between
// domain types and protobuf, maps storage errors onto gRPC status codes, and
// bridges the event pipeline's fan-out into server streams.
package grpcapi

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	guardianv1 "github.com/poune-mehdipour/smart-eyes/cloud-connector/api/gen/guardian/v1"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/commands"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// Store is the storage slice the gRPC surface needs.
type Store interface {
	GetDevice(ctx context.Context, id string) (domain.Device, error)
	ListDevices(ctx context.Context) ([]domain.Device, error)
	ListRecentEvents(ctx context.Context, limit int) ([]domain.HazardEvent, error)
}

// Server implements guardianv1.ConnectorServiceServer.
type Server struct {
	guardianv1.UnimplementedConnectorServiceServer
	store    Store
	events   *events.Service
	commands *commands.Service
	log      *slog.Logger
}

// New builds the server.
func New(store Store, ev *events.Service, cs *commands.Service, log *slog.Logger) *Server {
	return &Server{store: store, events: ev, commands: cs, log: log}
}

// Register attaches the server to a grpc.Server.
func (s *Server) Register(g *grpc.Server) {
	guardianv1.RegisterConnectorServiceServer(g, s)
}

func (s *Server) GetDevice(ctx context.Context, req *guardianv1.GetDeviceRequest) (*guardianv1.GetDeviceResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	d, err := s.store.GetDevice(ctx, req.GetId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "device not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "storage error")
	}
	return &guardianv1.GetDeviceResponse{Device: deviceToProto(d)}, nil
}

func (s *Server) ListDevices(ctx context.Context, _ *guardianv1.ListDevicesRequest) (*guardianv1.ListDevicesResponse, error) {
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "storage error")
	}
	out := make([]*guardianv1.Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, deviceToProto(d))
	}
	return &guardianv1.ListDevicesResponse{Devices: out}, nil
}

func (s *Server) ListRecentEvents(ctx context.Context, req *guardianv1.ListRecentEventsRequest) (*guardianv1.ListRecentEventsResponse, error) {
	evs, err := s.store.ListRecentEvents(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, "storage error")
	}
	out := make([]*guardianv1.HazardEvent, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventToProto(e))
	}
	return &guardianv1.ListRecentEventsResponse{Events: out}, nil
}

// StreamEvents subscribes to the live pipeline and forwards until the client
// goes away. Cancellation is cooperative: the subscriber channel is released
// on return, so an abandoned stream never leaks.
func (s *Server) StreamEvents(_ *guardianv1.StreamEventsRequest, stream guardianv1.ConnectorService_StreamEventsServer) error {
	ch, cancel := s.events.Subscribe()
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case e, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&guardianv1.StreamEventsResponse{Event: eventToProto(e)}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) SendCommand(ctx context.Context, req *guardianv1.SendCommandRequest) (*guardianv1.SendCommandResponse, error) {
	cmd, err := s.commands.Create(ctx, req.GetDeviceId(), commandTypeFromProto(req.GetType()),
		req.GetPayloadJson(), req.GetIdempotencyKey())
	if errors.Is(err, commands.ErrValidation) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "command creation failed")
	}
	return &guardianv1.SendCommandResponse{Command: commandToProto(cmd)}, nil
}

func (s *Server) GetCommand(ctx context.Context, req *guardianv1.GetCommandRequest) (*guardianv1.GetCommandResponse, error) {
	cmd, err := s.commands.Get(ctx, req.GetId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "command not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "storage error")
	}
	return &guardianv1.GetCommandResponse{Command: commandToProto(cmd)}, nil
}

// ---- Translation ----------------------------------------------------------

func deviceToProto(d domain.Device) *guardianv1.Device {
	out := &guardianv1.Device{
		Id:         d.ID,
		ExternalId: d.ExternalID,
		Provider:   string(d.Provider),
		Name:       d.Name,
		AppVersion: d.AppVersion,
	}
	switch d.Status {
	case domain.DeviceOnline:
		out.Status = guardianv1.DeviceStatus_DEVICE_STATUS_ONLINE
	case domain.DeviceOffline:
		out.Status = guardianv1.DeviceStatus_DEVICE_STATUS_OFFLINE
	default:
		out.Status = guardianv1.DeviceStatus_DEVICE_STATUS_UNKNOWN
	}
	if !d.LastSeenAt.IsZero() {
		out.LastSeenAt = timestamppb.New(d.LastSeenAt)
	}
	return out
}

func eventToProto(e domain.HazardEvent) *guardianv1.HazardEvent {
	return &guardianv1.HazardEvent{
		Id:          e.ID,
		DeviceId:    e.DeviceID,
		Source:      string(e.Source),
		Type:        hazardTypeToProto(e.Type),
		Confidence:  e.Confidence,
		FirstSeenAt: timestamppb.New(e.FirstSeenAt),
		ConfirmedAt: timestamppb.New(e.ConfirmedAt),
		ReceivedAt:  timestamppb.New(e.ReceivedAt),
	}
}

func hazardTypeToProto(t domain.HazardType) guardianv1.HazardType {
	switch t {
	case domain.HazardFire:
		return guardianv1.HazardType_HAZARD_TYPE_FIRE
	case domain.HazardSmoke:
		return guardianv1.HazardType_HAZARD_TYPE_SMOKE
	case domain.HazardFall:
		return guardianv1.HazardType_HAZARD_TYPE_FALL
	case domain.HazardIntrusion:
		return guardianv1.HazardType_HAZARD_TYPE_INTRUSION
	case domain.HazardCameraTampering:
		return guardianv1.HazardType_HAZARD_TYPE_CAMERA_TAMPERING
	default:
		return guardianv1.HazardType_HAZARD_TYPE_UNSPECIFIED
	}
}

func commandTypeFromProto(t guardianv1.CommandType) domain.CommandType {
	switch t {
	case guardianv1.CommandType_COMMAND_TYPE_GET_STATUS:
		return domain.CommandGetStatus
	case guardianv1.CommandType_COMMAND_TYPE_START_MONITORING:
		return domain.CommandStartMonitoring
	case guardianv1.CommandType_COMMAND_TYPE_STOP_MONITORING:
		return domain.CommandStopMonitoring
	case guardianv1.CommandType_COMMAND_TYPE_UPDATE_MONITORING_CONFIG:
		return domain.CommandUpdateConfig
	default:
		return domain.CommandType("")
	}
}

func commandToProto(c domain.Command) *guardianv1.Command {
	out := &guardianv1.Command{
		Id:            c.ID,
		DeviceId:      c.DeviceID,
		PayloadJson:   c.Payload,
		ResultMessage: c.ResultMessage,
		CreatedAt:     timestamppb.New(c.CreatedAt),
	}
	switch c.Type {
	case domain.CommandGetStatus:
		out.Type = guardianv1.CommandType_COMMAND_TYPE_GET_STATUS
	case domain.CommandStartMonitoring:
		out.Type = guardianv1.CommandType_COMMAND_TYPE_START_MONITORING
	case domain.CommandStopMonitoring:
		out.Type = guardianv1.CommandType_COMMAND_TYPE_STOP_MONITORING
	case domain.CommandUpdateConfig:
		out.Type = guardianv1.CommandType_COMMAND_TYPE_UPDATE_MONITORING_CONFIG
	}
	switch c.State {
	case domain.CommandPending:
		out.State = guardianv1.CommandState_COMMAND_STATE_PENDING
	case domain.CommandDelivered:
		out.State = guardianv1.CommandState_COMMAND_STATE_DELIVERED
	case domain.CommandAcknowledged:
		out.State = guardianv1.CommandState_COMMAND_STATE_ACKNOWLEDGED
	case domain.CommandFailed:
		out.State = guardianv1.CommandState_COMMAND_STATE_FAILED
	}
	if c.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*c.CompletedAt)
	}
	return out
}
