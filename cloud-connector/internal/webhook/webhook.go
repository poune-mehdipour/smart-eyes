// Package webhook ingests provider push deliveries. The processing order is
// deliberate and security-relevant:
//
//  1. bound read      — a request larger than the cap is rejected unread
//  2. verify signature — unauthenticated garbage never touches a parser
//  3. parse & validate — malformed-but-authentic payloads → 400 + dead letter
//  4. timestamp window — replayed old deliveries are rejected
//  5. dedup gate       — same delivery ID twice → 200 (idempotent), counted
//  6. process          — hazard events into the pipeline, heartbeats to devices
//
// Answering 200 to duplicates matters: providers retry until they see
// success, so an error answer to a duplicate would cause infinite redelivery.
package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider/betagrid"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// Store is the storage slice this package needs.
type Store interface {
	RecordWebhookDelivery(ctx context.Context, p domain.ProviderName, deliveryID string) error
	UpsertDevice(ctx context.Context, d domain.Device) (string, error)
	TouchDevice(ctx context.Context, deviceID string, status domain.DeviceStatus, at time.Time) error
}

// Handler serves POST /v1/webhooks/betagrid.
type Handler struct {
	store    Store
	events   *events.Service
	log      *slog.Logger
	metrics  *observability.Metrics
	secret   string
	maxSkew  time.Duration
	maxBytes int64
	now      func() time.Time
}

// New builds the handler. secret empty means the BetaGrid integration is not
// configured; deliveries are rejected 503 rather than accepted unsigned.
func New(store Store, ev *events.Service, log *slog.Logger, m *observability.Metrics,
	secret string, maxSkew time.Duration, maxBytes int64) *Handler {
	return &Handler{
		store: store, events: ev, log: log, metrics: m,
		secret: secret, maxSkew: maxSkew, maxBytes: maxBytes, now: time.Now,
	}
}

// BetaGrid handles one delivery from the simulated BetaGrid provider.
func (h *Handler) BetaGrid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := observability.LoggerFrom(ctx, h.log)
	pname := domain.ProviderBetaGrid
	reject := func(status int, reason string) {
		h.metrics.WebhookRejected.WithLabelValues(string(pname), reason).Inc()
		log.Warn("webhook_rejected", "provider", string(pname), "reason", reason)
		http.Error(w, reason, status)
	}

	if h.secret == "" {
		reject(http.StatusServiceUnavailable, "webhook secret not configured")
		return
	}

	// 1. Bound read.
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBytes+1))
	if err != nil {
		reject(http.StatusBadRequest, "unreadable body")
		return
	}
	if int64(len(body)) > h.maxBytes {
		reject(http.StatusRequestEntityTooLarge, "body too large")
		return
	}

	// 2. Authenticate before parsing anything.
	if !betagrid.VerifySignature(body, r.Header.Get(betagrid.SignatureHeader), h.secret) {
		reject(http.StatusUnauthorized, "bad signature")
		return
	}

	// 3. Parse and validate. Authentic-but-malformed goes to the dead-letter
	// table so it can be diagnosed; it is never silently accepted.
	delivery, err := betagrid.Parse(body, h.now().UTC())
	if err != nil {
		h.events.DeadLetter(ctx, pname, err.Error(), body)
		reject(http.StatusBadRequest, "malformed payload")
		return
	}

	// 4. Timestamp window: bounds replay of captured (signed) deliveries.
	if skew := h.now().Sub(delivery.SentAt).Abs(); skew > h.maxSkew {
		reject(http.StatusBadRequest, "timestamp outside window")
		return
	}

	// 5. Idempotency gate.
	switch err := h.store.RecordWebhookDelivery(ctx, pname, delivery.MsgID); {
	case errors.Is(err, storage.ErrDuplicate):
		h.metrics.WebhookDuplicates.WithLabelValues(string(pname)).Inc()
		log.Info("webhook_duplicate", "provider", string(pname), "delivery_id", delivery.MsgID)
		writeOK(w) // success: the provider must stop retrying
		return
	case err != nil:
		reject(http.StatusInternalServerError, "storage error")
		return
	}

	// 6. Process.
	switch {
	case delivery.Event != nil:
		if _, err := h.events.AcceptFromProvider(ctx, *delivery.Event); err != nil {
			var rej events.ErrRejected
			if errors.As(err, &rej) {
				h.events.DeadLetter(ctx, pname, rej.Error(), body)
				reject(http.StatusBadRequest, "invalid event")
				return
			}
			reject(http.StatusInternalServerError, "processing error")
			return
		}
	case delivery.Heartbeat != nil:
		id, err := h.store.UpsertDevice(ctx, domain.Device{
			ExternalID: delivery.Heartbeat.ExternalDeviceID,
			Provider:   pname,
			Status:     delivery.Heartbeat.Status,
			LastSeenAt: delivery.SentAt,
		})
		if err == nil {
			err = h.store.TouchDevice(ctx, id, delivery.Heartbeat.Status, delivery.SentAt)
		}
		if err != nil {
			reject(http.StatusInternalServerError, "storage error")
			return
		}
	}
	writeOK(w)
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}
