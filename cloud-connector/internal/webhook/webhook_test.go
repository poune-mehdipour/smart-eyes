package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider/betagrid"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

const secret = "test-webhook-secret"

// fakeStore implements webhook.Store and events.Store in memory.
type fakeStore struct {
	deliveries  map[string]bool
	events      []domain.HazardEvent
	eventKeys   map[string]bool
	deadLetters []string
	devices     map[string]string // provider/external -> id
	touched     int
}

func newFakeStore() *fakeStore {
	return &fakeStore{deliveries: map[string]bool{}, eventKeys: map[string]bool{}, devices: map[string]string{}}
}

func (f *fakeStore) RecordWebhookDelivery(_ context.Context, p domain.ProviderName, id string) error {
	key := string(p) + "/" + id
	if f.deliveries[key] {
		return storage.ErrDuplicate
	}
	f.deliveries[key] = true
	return nil
}

func (f *fakeStore) UpsertDevice(_ context.Context, d domain.Device) (string, error) {
	key := string(d.Provider) + "/" + d.ExternalID
	if id, ok := f.devices[key]; ok {
		return id, nil
	}
	id := fmt.Sprintf("dev-%d", len(f.devices)+1)
	f.devices[key] = id
	return id, nil
}

func (f *fakeStore) TouchDevice(_ context.Context, _ string, _ domain.DeviceStatus, _ time.Time) error {
	f.touched++
	return nil
}

func (f *fakeStore) InsertHazardEvent(_ context.Context, e domain.HazardEvent) (domain.HazardEvent, error) {
	key := string(e.Source) + "/" + e.DedupKey
	if f.eventKeys[key] {
		return domain.HazardEvent{}, storage.ErrDuplicate
	}
	f.eventKeys[key] = true
	f.events = append(f.events, e)
	return e, nil
}

func (f *fakeStore) InsertDeadLetter(_ context.Context, _ domain.ProviderName, reason string, _ []byte) error {
	f.deadLetters = append(f.deadLetters, reason)
	return nil
}

func newHandler(t *testing.T, store *fakeStore) *Handler {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	ev := events.New(store, log, m)
	return New(store, ev, log, m, secret, 5*time.Minute, 1<<20)
}

func hazardBody(msgID string, sentAt time.Time) []byte {
	b, _ := json.Marshal(map[string]any{
		"msg_id": msgID, "sent_at": sentAt.Unix(), "kind": "hazard",
		"data": map[string]any{
			"device": "bg-unit-9", "hazard": "FLAME", "certainty": "high",
			"started": sentAt.Add(-10 * time.Second).Unix(), "verified": sentAt.Unix(),
		},
	})
	return b
}

func deliver(h *Handler, body []byte, sign bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/betagrid", bytes.NewReader(body))
	if sign {
		req.Header.Set(betagrid.SignatureHeader, betagrid.Sign(body, secret))
	}
	rec := httptest.NewRecorder()
	h.BetaGrid(rec, req)
	return rec
}

func TestValidHazardDeliveryIsAccepted(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	rec := deliver(h, hazardBody("bg-1", time.Now()), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.events) != 1 || store.events[0].Type != domain.HazardFire {
		t.Fatalf("events = %+v", store.events)
	}
}

func TestUnsignedDeliveryIsRejectedBeforeProcessing(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	rec := deliver(h, hazardBody("bg-1", time.Now()), false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(store.events) != 0 || len(store.deliveries) != 0 {
		t.Fatal("unauthenticated delivery must leave no trace")
	}
}

func TestTamperedBodyIsRejected(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	body := hazardBody("bg-1", time.Now())
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(append(body, ' ')))
	req.Header.Set(betagrid.SignatureHeader, betagrid.Sign(body, secret))
	rec := httptest.NewRecorder()
	h.BetaGrid(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestDuplicateDeliveryIsIdempotent(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	body := hazardBody("bg-dup", time.Now())

	first := deliver(h, body, true)
	second := deliver(h, body, true)

	// The duplicate must be answered 200 (so the sender stops retrying)…
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("codes = %d, %d; want 200, 200", first.Code, second.Code)
	}
	// …but processed exactly once.
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
}

func TestMalformedButSignedGoesToDeadLetter(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	body := []byte(`{"msg_id":"bg-x","sent_at":` + fmt.Sprint(time.Now().Unix()) + `,"kind":"hazard","data":{"device":"d","hazard":"GHOST","certainty":"high","verified":1}}`)
	rec := deliver(h, body, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.deadLetters) != 1 {
		t.Fatalf("dead letters = %v, want 1 entry", store.deadLetters)
	}
	if len(store.events) != 0 {
		t.Fatal("malformed payload must never become an event")
	}
}

func TestStaleTimestampIsRejected(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	rec := deliver(h, hazardBody("bg-old", time.Now().Add(-time.Hour)), true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for stale timestamp", rec.Code)
	}
	if len(store.events) != 0 {
		t.Fatal("stale delivery must not be processed")
	}
}

func TestHeartbeatUpdatesDevice(t *testing.T) {
	store := newFakeStore()
	h := newHandler(t, store)
	body, _ := json.Marshal(map[string]any{
		"msg_id": "bg-hb", "sent_at": time.Now().Unix(), "kind": "heartbeat",
		"data": map[string]any{"device": "bg-unit-9", "state": "down"},
	})
	rec := deliver(h, body, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if store.touched != 1 {
		t.Fatalf("touched = %d, want 1", store.touched)
	}
	if len(store.events) != 0 {
		t.Fatal("heartbeat must not create a hazard event")
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	store := newFakeStore()
	h := New(store, events.New(store, slog.New(slog.DiscardHandler), observability.NewMetrics()),
		slog.New(slog.DiscardHandler), observability.NewMetrics(), secret, 5*time.Minute, 64)
	rec := deliver(h, hazardBody("bg-big", time.Now()), true)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestUnconfiguredSecretRejectsAll(t *testing.T) {
	store := newFakeStore()
	h := New(store, events.New(store, slog.New(slog.DiscardHandler), observability.NewMetrics()),
		slog.New(slog.DiscardHandler), observability.NewMetrics(), "", 5*time.Minute, 1<<20)
	rec := deliver(h, hazardBody("bg-1", time.Now()), true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when secret unset", rec.Code)
	}
}
