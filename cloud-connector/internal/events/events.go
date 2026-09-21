// Package events is the single pipeline every hazard event flows through,
// whatever its origin (edge ingest, provider poll, webhook):
//
//	validate → resolve device → persist (idempotent) → publish → count
//
// Having one pipeline means idempotency, validation and metrics behave
// identically for all sources, and the gRPC event stream sees everything.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// Store is the slice of the storage API this package needs; *storage.Store
// satisfies it, and tests substitute a fake.
type Store interface {
	InsertHazardEvent(ctx context.Context, e domain.HazardEvent) (domain.HazardEvent, error)
	UpsertDevice(ctx context.Context, d domain.Device) (string, error)
	InsertDeadLetter(ctx context.Context, source domain.ProviderName, reason string, payload []byte) error
}

// Service runs the pipeline and fans accepted events out to subscribers.
type Service struct {
	store   Store
	log     *slog.Logger
	metrics *observability.Metrics

	mu   sync.Mutex
	subs map[chan domain.HazardEvent]struct{}
}

// New builds the service.
func New(store Store, log *slog.Logger, m *observability.Metrics) *Service {
	return &Service{store: store, log: log, metrics: m, subs: map[chan domain.HazardEvent]struct{}{}}
}

// ErrRejected wraps a validation failure; the transport layer maps it to 400.
type ErrRejected struct{ Reason error }

func (e ErrRejected) Error() string { return "event rejected: " + e.Reason.Error() }
func (e ErrRejected) Unwrap() error { return e.Reason }

// Accept runs one event with a known connector DeviceID through the
// pipeline. Duplicates are counted and reported as accepted (idempotent
// success) with duplicate=true.
func (s *Service) Accept(ctx context.Context, e domain.HazardEvent) (duplicate bool, err error) {
	src := string(e.Source)
	s.metrics.EventsReceived.WithLabelValues(src).Inc()

	if err := e.Validate(); err != nil {
		s.metrics.EventsFailed.WithLabelValues(src, "validation").Inc()
		return false, ErrRejected{Reason: err}
	}

	stored, err := s.store.InsertHazardEvent(ctx, e)
	switch {
	case errors.Is(err, storage.ErrDuplicate):
		s.metrics.EventsDuplicate.WithLabelValues(src).Inc()
		observability.LoggerFrom(ctx, s.log).Debug("duplicate_event",
			"source", src, "dedup_key", e.DedupKey)
		return true, nil
	case err != nil:
		s.metrics.EventsFailed.WithLabelValues(src, "storage").Inc()
		return false, err
	}

	s.metrics.EventsProcessed.WithLabelValues(src).Inc()
	observability.LoggerFrom(ctx, s.log).Info("hazard_event_accepted",
		"source", src, "device_id", stored.DeviceID, "type", string(stored.Type),
		"confidence", stored.Confidence, "event_id", stored.ID)
	s.publish(stored)
	return false, nil
}

// AcceptFromProvider resolves the provider's device ID (registering a stub
// device on first sight — providers are the authority on their own fleet)
// and runs the pipeline. Used by the polling loop and the webhook handler.
func (s *Service) AcceptFromProvider(ctx context.Context, ne provider.NormalizedEvent) (duplicate bool, err error) {
	deviceID, err := s.store.UpsertDevice(ctx, domain.Device{
		ExternalID: ne.ExternalDeviceID,
		Provider:   ne.Event.Source,
		Status:     domain.DeviceUnknown,
		LastSeenAt: ne.Event.ConfirmedAt,
	})
	if err != nil {
		s.metrics.EventsFailed.WithLabelValues(string(ne.Event.Source), "device_resolution").Inc()
		return false, fmt.Errorf("resolving device %q: %w", ne.ExternalDeviceID, err)
	}
	e := ne.Event
	e.DeviceID = deviceID
	return s.Accept(ctx, e)
}

// DeadLetter records a payload that authenticated correctly but can never be
// processed. It is deliberately separate from Accept: dead letters are for
// terminal, diagnosable garbage, not for transient failures.
func (s *Service) DeadLetter(ctx context.Context, source domain.ProviderName, reason string, payload []byte) {
	s.metrics.EventsFailed.WithLabelValues(string(source), "dead_letter").Inc()
	if err := s.store.InsertDeadLetter(ctx, source, reason, payload); err != nil {
		observability.LoggerFrom(ctx, s.log).Error("dead_letter_write_failed", "error", err.Error())
	}
}

// ---- Fan-out for gRPC StreamEvents ---------------------------------------

// Subscribe returns a channel receiving every event accepted from now on,
// and a cancel func. The channel has a small buffer; a subscriber that stops
// draining loses events rather than blocking the pipeline (documented
// behavior — the stream is a live feed, not a replay log).
func (s *Service) Subscribe() (<-chan domain.HazardEvent, func()) {
	ch := make(chan domain.HazardEvent, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
	return ch, cancel
}

func (s *Service) publish(e domain.HazardEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop for them, never stall ingestion
		}
	}
}
