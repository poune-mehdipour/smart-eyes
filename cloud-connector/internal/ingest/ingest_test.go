package ingest

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

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/commands"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

const token = "edge-test-token"

// fakeStore backs ingest, events and commands in memory.
type fakeStore struct {
	devices   map[string]domain.Device // by id
	byExt     map[string]string
	telemetry []domain.TelemetryEvent
	events    []domain.HazardEvent
	eventKeys map[string]bool
	commands  map[string]*domain.Command
	seq       int
}

func newFakeStore() *fakeStore {
	return &fakeStore{devices: map[string]domain.Device{}, byExt: map[string]string{},
		eventKeys: map[string]bool{}, commands: map[string]*domain.Command{}}
}

func (f *fakeStore) UpsertDevice(_ context.Context, d domain.Device) (string, error) {
	key := string(d.Provider) + "/" + d.ExternalID
	if id, ok := f.byExt[key]; ok {
		return id, nil
	}
	f.seq++
	id := fmt.Sprintf("dev-%d", f.seq)
	d.ID = id
	f.devices[id] = d
	f.byExt[key] = id
	return id, nil
}

func (f *fakeStore) InsertTelemetry(_ context.Context, t domain.TelemetryEvent) error {
	f.telemetry = append(f.telemetry, t)
	return nil
}

func (f *fakeStore) FindDeviceByExternalID(_ context.Context, p domain.ProviderName, ext string) (domain.Device, error) {
	if id, ok := f.byExt[string(p)+"/"+ext]; ok {
		return f.devices[id], nil
	}
	return domain.Device{}, storage.ErrNotFound
}

func (f *fakeStore) GetDevice(_ context.Context, id string) (domain.Device, error) {
	d, ok := f.devices[id]
	if !ok {
		return domain.Device{}, storage.ErrNotFound
	}
	return d, nil
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

func (f *fakeStore) InsertDeadLetter(context.Context, domain.ProviderName, string, []byte) error {
	return nil
}

func (f *fakeStore) CreateCommand(_ context.Context, c domain.Command) (domain.Command, error) {
	f.seq++
	c.ID = fmt.Sprintf("cmd-%d", f.seq)
	c.State = domain.CommandPending
	f.commands[c.ID] = &c
	return c, nil
}

func (f *fakeStore) GetCommand(_ context.Context, id string) (domain.Command, error) {
	if c, ok := f.commands[id]; ok {
		return *c, nil
	}
	return domain.Command{}, storage.ErrNotFound
}

func (f *fakeStore) FetchPendingCommands(_ context.Context, deviceID string, limit int) ([]domain.Command, error) {
	var out []domain.Command
	for _, c := range f.commands {
		if c.DeviceID == deviceID && c.State == domain.CommandPending && len(out) < limit {
			c.State = domain.CommandDelivered
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakeStore) TransitionCommand(_ context.Context, id string, to domain.CommandState, msg string) (domain.Command, error) {
	c, ok := f.commands[id]
	if !ok {
		return domain.Command{}, storage.ErrNotFound
	}
	if !domain.CanTransition(c.State, to) {
		return domain.Command{}, storage.ErrInvalidTransition
	}
	c.State = to
	c.ResultMessage = msg
	return *c, nil
}

func (f *fakeStore) ExpireCommands(context.Context) (int64, error) { return 0, nil }

// fixture builds the API over an httptest server.
func fixture(t *testing.T) (*fakeStore, *httptest.Server) {
	t.Helper()
	store := newFakeStore()
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	ev := events.New(store, log, m)
	cs := commands.New(store, log, m, time.Hour, nil)
	api := New(store, ev, cs, log, m, token, 1<<20)

	mux := http.NewServeMux()
	api.Register(mux, func(h http.Handler, _ string) http.Handler { return h })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return store, srv
}

func post(t *testing.T, srv *httptest.Server, path string, body any, authed bool) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if authed {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func validEvent(eventID string) map[string]any {
	return map[string]any{
		"deviceId": "pixel-7-test", "eventId": eventID, "type": "FIRE", "confidence": 0.83,
		"firstSeenMs": 1758441590000, "confirmedAtMs": 1758441600000, "appVersion": "1.0",
	}
}

func TestTelemetryRegistersDeviceAndStores(t *testing.T) {
	store, srv := fixture(t)
	resp := post(t, srv, "/v1/edge/telemetry", map[string]any{
		"deviceId": "pixel-7-test", "reportedAtMs": 1758441600000, "monitoringStatus": "MONITORING",
		"modelStatus": "READY", "inferenceLatencyMs": 42, "processedFps": 4,
		"droppedFrames": 7, "confirmedEvents": 1, "appVersion": "1.0",
	}, true)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(store.telemetry) != 1 || len(store.devices) != 1 {
		t.Fatalf("telemetry=%d devices=%d", len(store.telemetry), len(store.devices))
	}
	tel := store.telemetry[0]
	if tel.MonitoringStatus != domain.MonitoringActive || tel.InferenceLatencyMs != 42 {
		t.Fatalf("telemetry = %+v", tel)
	}
}

func TestEventAcceptedAndDuplicateFlagged(t *testing.T) {
	store, srv := fixture(t)

	first := post(t, srv, "/v1/edge/events", validEvent("evt-1"), true)
	second := post(t, srv, "/v1/edge/events", validEvent("evt-1"), true)
	if first.StatusCode != http.StatusAccepted || second.StatusCode != http.StatusAccepted {
		t.Fatalf("codes = %d, %d", first.StatusCode, second.StatusCode)
	}
	var body struct {
		Duplicate bool `json:"duplicate"`
	}
	_ = json.NewDecoder(second.Body).Decode(&body)
	if !body.Duplicate {
		t.Fatal("second delivery must be flagged duplicate")
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
}

func TestEventValidationRejects(t *testing.T) {
	_, srv := fixture(t)
	tests := []struct {
		name string
		mut  func(map[string]any)
	}{
		{"missing eventId", func(m map[string]any) { delete(m, "eventId") }},
		{"unknown type", func(m map[string]any) { m["type"] = "TSUNAMI" }},
		{"confidence above 1", func(m map[string]any) { m["confidence"] = 1.5 }},
		{"missing confirmedAt", func(m map[string]any) { delete(m, "confirmedAtMs") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := validEvent("evt-x")
			tt.mut(body)
			resp := post(t, srv, "/v1/edge/events", body, true)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestMalformedJSONIs400(t *testing.T) {
	_, srv := fixture(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/edge/events", bytes.NewReader([]byte("{nope")))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestMissingBearerTokenIs401(t *testing.T) {
	store, srv := fixture(t)
	resp := post(t, srv, "/v1/edge/events", validEvent("evt-1"), false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if len(store.events) != 0 {
		t.Fatal("unauthenticated event must not be stored")
	}
}

func TestCommandPollAndResultRoundTrip(t *testing.T) {
	store, srv := fixture(t)
	// Register the device via telemetry, then enqueue a command directly.
	post(t, srv, "/v1/edge/telemetry", map[string]any{
		"deviceId": "pixel-7-test", "reportedAtMs": 1758441600000, "monitoringStatus": "IDLE",
	}, true)
	deviceID := store.byExt["guardian-edge/pixel-7-test"]
	cmd, _ := store.CreateCommand(context.Background(), domain.Command{
		DeviceID: deviceID, Type: domain.CommandGetStatus, ExpiresAt: time.Now().Add(time.Hour),
	})

	// Poll.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/edge/commands?deviceId=pixel-7-test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var poll struct {
		Commands []struct{ ID, Type string } `json:"commands"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&poll)
	if len(poll.Commands) != 1 || poll.Commands[0].ID != cmd.ID {
		t.Fatalf("poll = %+v", poll)
	}

	// Report result.
	res := post(t, srv, "/v1/edge/commands/"+cmd.ID+"/result", map[string]any{"ok": true, "message": "done"}, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("result status = %d", res.StatusCode)
	}
	if store.commands[cmd.ID].State != domain.CommandAcknowledged {
		t.Fatalf("state = %v", store.commands[cmd.ID].State)
	}

	// A second result for the same command conflicts.
	res2 := post(t, srv, "/v1/edge/commands/"+cmd.ID+"/result", map[string]any{"ok": true}, true)
	if res2.StatusCode != http.StatusConflict {
		t.Fatalf("second result status = %d, want 409", res2.StatusCode)
	}
}

func TestPollUnknownDeviceIsEmptyNotError(t *testing.T) {
	_, srv := fixture(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/edge/commands?deviceId=never-seen", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
