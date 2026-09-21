// Package commands owns the cloud→edge command lifecycle:
//
//	PENDING ──(device polls / provider accepts)──▶ DELIVERED ──▶ ACKNOWLEDGED
//	   │                                              │
//	   └────────────────(expiry / failure)────────────┴────────▶ FAILED
//
// Guardian edge devices PULL commands (they poll while online — a phone that
// must work offline cannot hold a broker connection), so delivery for them is
// "the device fetched it". Provider-owned devices are PUSH: the adapter's
// CommandSender delivers immediately if the provider supports commands at
// all. A provider without the capability rejects at creation time with a
// clear error instead of leaving a command pending forever.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// Store is the storage slice this package needs.
type Store interface {
	GetDevice(ctx context.Context, id string) (domain.Device, error)
	FindDeviceByExternalID(ctx context.Context, p domain.ProviderName, externalID string) (domain.Device, error)
	CreateCommand(ctx context.Context, c domain.Command) (domain.Command, error)
	GetCommand(ctx context.Context, id string) (domain.Command, error)
	FetchPendingCommands(ctx context.Context, deviceID string, limit int) ([]domain.Command, error)
	TransitionCommand(ctx context.Context, id string, to domain.CommandState, msg string) (domain.Command, error)
	ExpireCommands(ctx context.Context) (int64, error)
}

// ErrValidation marks caller mistakes (unknown type, bad payload, unknown
// device); transports map it to 400 / InvalidArgument.
var ErrValidation = errors.New("invalid command")

// Service coordinates persistence and provider dispatch.
type Service struct {
	store   Store
	log     *slog.Logger
	metrics *observability.Metrics
	ttl     time.Duration
	// senders maps provider name → command capability, for provider-owned
	// devices. Guardian edge devices are not in this map: they poll.
	senders map[domain.ProviderName]provider.CommandSender
}

// New builds the service.
func New(store Store, log *slog.Logger, m *observability.Metrics, ttl time.Duration,
	senders map[domain.ProviderName]provider.CommandSender) *Service {
	if senders == nil {
		senders = map[domain.ProviderName]provider.CommandSender{}
	}
	return &Service{store: store, log: log, metrics: m, ttl: ttl, senders: senders}
}

// Create validates and enqueues a command for a device, dispatching
// immediately when the owning provider supports push delivery. Reusing an
// idempotency key returns the original command unchanged.
func (s *Service) Create(ctx context.Context, deviceID string, typ domain.CommandType, payload, idemKey string) (domain.Command, error) {
	if !domain.ValidCommandType(typ) {
		return domain.Command{}, fmt.Errorf("%w: unknown type %q", ErrValidation, typ)
	}
	if payload != "" && !json.Valid([]byte(payload)) {
		return domain.Command{}, fmt.Errorf("%w: payload is not valid JSON", ErrValidation)
	}

	device, err := s.store.GetDevice(ctx, deviceID)
	if errors.Is(err, storage.ErrNotFound) {
		return domain.Command{}, fmt.Errorf("%w: unknown device %q", ErrValidation, deviceID)
	}
	if err != nil {
		return domain.Command{}, err
	}

	// A command to a device whose provider cannot deliver commands is a
	// caller error at creation time — not a command that rots in PENDING.
	sender, hasSender := s.senders[device.Provider]
	if device.Provider != domain.ProviderGuardianEdge && !hasSender {
		return domain.Command{}, fmt.Errorf("%w: provider %q does not accept commands", ErrValidation, device.Provider)
	}

	cmd, err := s.store.CreateCommand(ctx, domain.Command{
		DeviceID:       deviceID,
		Type:           typ,
		Payload:        payload,
		IdempotencyKey: idemKey,
		ExpiresAt:      time.Now().Add(s.ttl),
	})
	if errors.Is(err, storage.ErrDuplicate) {
		observability.LoggerFrom(ctx, s.log).Info("command_idempotent_replay",
			"command_id", cmd.ID, "idempotency_key", idemKey)
		return cmd, nil
	}
	if err != nil {
		return domain.Command{}, err
	}
	s.metrics.CommandsCreated.WithLabelValues(string(typ)).Inc()
	observability.LoggerFrom(ctx, s.log).Info("command_created",
		"command_id", cmd.ID, "device_id", deviceID, "type", string(typ))

	if hasSender {
		return s.dispatch(ctx, sender, device, cmd)
	}
	return cmd, nil // guardian-edge: stays PENDING until the device polls
}

// dispatch pushes a command through a provider adapter and records the
// outcome. Adapter errors (already retried with backoff inside the adapter)
// fail the command with the reason preserved.
func (s *Service) dispatch(ctx context.Context, sender provider.CommandSender, device domain.Device, cmd domain.Command) (domain.Command, error) {
	state, msg, err := sender.SendCommand(ctx, device.ExternalID, cmd)
	if err != nil {
		failed, terr := s.store.TransitionCommand(ctx, cmd.ID, domain.CommandFailed, err.Error())
		if terr != nil {
			return cmd, terr
		}
		s.metrics.CommandTransitions.WithLabelValues(string(domain.CommandFailed)).Inc()
		s.metrics.CommandFailures.Inc()
		observability.LoggerFrom(ctx, s.log).Warn("command_dispatch_failed",
			"command_id", cmd.ID, "provider", string(device.Provider), "error", err.Error())
		return failed, nil // the command's failure is recorded state, not a transport error
	}
	updated, terr := s.store.TransitionCommand(ctx, cmd.ID, state, msg)
	if terr != nil {
		return cmd, terr
	}
	s.metrics.CommandTransitions.WithLabelValues(string(state)).Inc()
	return updated, nil
}

// Poll claims pending commands for an edge device (they become DELIVERED).
func (s *Service) Poll(ctx context.Context, deviceID string, limit int) ([]domain.Command, error) {
	cmds, err := s.store.FetchPendingCommands(ctx, deviceID, limit)
	if err != nil {
		return nil, err
	}
	for range cmds {
		s.metrics.CommandTransitions.WithLabelValues(string(domain.CommandDelivered)).Inc()
	}
	return cmds, nil
}

// ReportResult records the device-reported outcome of a delivered command.
func (s *Service) ReportResult(ctx context.Context, commandID string, ok bool, message string) (domain.Command, error) {
	to := domain.CommandAcknowledged
	if !ok {
		to = domain.CommandFailed
	}
	cmd, err := s.store.TransitionCommand(ctx, commandID, to, message)
	if err != nil {
		return domain.Command{}, err
	}
	s.metrics.CommandTransitions.WithLabelValues(string(to)).Inc()
	if !ok {
		s.metrics.CommandFailures.Inc()
	}
	observability.LoggerFrom(ctx, s.log).Info("command_result",
		"command_id", commandID, "state", string(to))
	return cmd, nil
}

// Get returns one command.
func (s *Service) Get(ctx context.Context, id string) (domain.Command, error) {
	return s.store.GetCommand(ctx, id)
}

// RunReaper periodically fails expired commands until ctx is done.
func (s *Service) RunReaper(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.store.ExpireCommands(ctx)
			if err != nil {
				s.log.Error("command_reaper_failed", "error", err.Error())
				continue
			}
			if n > 0 {
				for i := int64(0); i < n; i++ {
					s.metrics.CommandTransitions.WithLabelValues(string(domain.CommandFailed)).Inc()
					s.metrics.CommandFailures.Inc()
				}
				s.log.Info("commands_expired", "count", n)
			}
		}
	}
}
