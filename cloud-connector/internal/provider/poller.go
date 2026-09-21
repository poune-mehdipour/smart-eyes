package provider

import (
	"context"
	"log/slog"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/reliability"
)

// EventSink is where polled, normalized events go — implemented by the
// events.Service pipeline (defined here as an interface to avoid a cycle).
type EventSink interface {
	AcceptFromProvider(ctx context.Context, ne NormalizedEvent) (duplicate bool, err error)
}

// DeviceSink persists polled device state.
type DeviceSink interface {
	UpsertDevice(ctx context.Context, d domain.Device) (string, error)
}

// Poller drives one polling provider on a fixed interval. Reliability model:
// the adapter retries transient failures internally with backoff; if a whole
// cycle still fails, the poller logs it, counts it, keeps its watermark, and
// simply tries again next tick — a provider outage delays that provider's
// events and affects nothing else.
type Poller struct {
	Provider Provider
	Devices  DeviceSink
	Events   EventSink
	Log      *slog.Logger
	Metrics  *observability.Metrics
	Interval time.Duration

	// Overlap is subtracted from the watermark each cycle so that events
	// confirmed just around a poll are never missed; idempotency makes the
	// resulting redelivery harmless. Default 1 minute.
	Overlap time.Duration
}

// Run polls until ctx is done. The first cycle runs immediately.
func (p *Poller) Run(ctx context.Context) {
	if p.Overlap <= 0 {
		p.Overlap = time.Minute
	}
	name := string(p.Provider.Name())
	// Start looking one interval back: everything older predates this
	// process and is the job of a catch-up mechanism, not the live poller.
	since := time.Now().UTC().Add(-p.Interval)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		since = p.cycle(ctx, name, since)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// cycle runs one poll and returns the next watermark.
func (p *Poller) cycle(ctx context.Context, name string, since time.Time) time.Time {
	cycleStart := time.Now().UTC()

	if dp, ok := p.Provider.(DevicePoller); ok {
		p.Metrics.ProviderRequests.WithLabelValues(name, "fetch_devices").Inc()
		devices, err := dp.FetchDevices(ctx)
		if err != nil {
			p.Metrics.ProviderRequestErrors.WithLabelValues(name, "fetch_devices", errKind(err)).Inc()
			p.Log.Warn("provider_poll_devices_failed", "provider", name, "error", err.Error())
		} else {
			for _, d := range devices {
				if _, err := p.Devices.UpsertDevice(ctx, d); err != nil {
					p.Log.Warn("provider_device_upsert_failed", "provider", name, "external_id", d.ExternalID, "error", err.Error())
				}
			}
		}
	}

	if ep, ok := p.Provider.(EventPoller); ok {
		p.Metrics.ProviderRequests.WithLabelValues(name, "fetch_events").Inc()
		evs, err := ep.FetchEvents(ctx, since.Add(-p.Overlap))
		if err != nil {
			p.Metrics.ProviderRequestErrors.WithLabelValues(name, "fetch_events", errKind(err)).Inc()
			p.Log.Warn("provider_poll_events_failed", "provider", name, "error", err.Error())
			// Keep the old watermark: the next cycle re-covers this window.
			return since
		}
		for _, ne := range evs {
			if _, err := p.Events.AcceptFromProvider(ctx, ne); err != nil {
				p.Log.Warn("provider_event_rejected", "provider", name, "dedup_key", ne.Event.DedupKey, "error", err.Error())
			}
		}
	}
	return cycleStart
}

func errKind(err error) string {
	if reliability.IsPermanent(err) {
		return "permanent"
	}
	return "transient"
}
