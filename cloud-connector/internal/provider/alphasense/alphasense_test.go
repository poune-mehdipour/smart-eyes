package alphasense

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/oauth"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// ---- Pure normalization tests ---------------------------------------------

func TestNormalizeUnit(t *testing.T) {
	tests := []struct {
		name    string
		unit    wireUnit
		want    domain.DeviceStatus
		wantErr bool
	}{
		{"green is online", wireUnit{UnitRef: "u1", Health: "green"}, domain.DeviceOnline, false},
		{"amber is online (degraded)", wireUnit{UnitRef: "u1", Health: "amber"}, domain.DeviceOnline, false},
		{"red is offline", wireUnit{UnitRef: "u1", Health: "red"}, domain.DeviceOffline, false},
		{"unknown health is unknown", wireUnit{UnitRef: "u1", Health: "purple"}, domain.DeviceUnknown, false},
		{"missing unit_ref is an error", wireUnit{Health: "green"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := normalizeUnit(tt.unit, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && d.Status != tt.want {
				t.Fatalf("status = %v, want %v", d.Status, tt.want)
			}
			if err == nil && d.Provider != domain.ProviderAlphaSense {
				t.Fatalf("provider = %v", d.Provider)
			}
		})
	}
}

func TestNormalizeAlert(t *testing.T) {
	valid := wireAlert{
		AlertID: "a-1", UnitRef: "u-1", Category: "fire", Score: 87.5,
		RaisedAt: "2026-09-21T11:59:40Z", Confirmed: "2026-09-21T12:00:00Z",
	}

	t.Run("valid fire alert", func(t *testing.T) {
		ne, err := normalizeAlert(valid, now)
		if err != nil {
			t.Fatal(err)
		}
		e := ne.Event
		// Score 0–100 must become confidence 0–1.
		if e.Confidence != 0.875 {
			t.Fatalf("confidence = %v, want 0.875", e.Confidence)
		}
		if e.Type != domain.HazardFire || e.Source != domain.ProviderAlphaSense || e.DedupKey != "a-1" {
			t.Fatalf("normalized wrong: %+v", e)
		}
	})

	mut := func(f func(*wireAlert)) wireAlert { a := valid; f(&a); return a }
	bad := []struct {
		name  string
		alert wireAlert
	}{
		{"unknown category", mut(func(a *wireAlert) { a.Category = "meteor" })},
		{"missing alert_id", mut(func(a *wireAlert) { a.AlertID = "" })},
		{"missing unit_ref", mut(func(a *wireAlert) { a.UnitRef = "" })},
		{"score above 100", mut(func(a *wireAlert) { a.Score = 150 })},
		{"negative score", mut(func(a *wireAlert) { a.Score = -1 })},
		{"garbage confirmed_at", mut(func(a *wireAlert) { a.Confirmed = "yesterday-ish" })},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeAlert(tt.alert, now); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// ---- HTTP behavior tests ---------------------------------------------------

// fixture wires a fake AlphaSense API + token endpoint behind a real Client.
func fixture(t *testing.T, api http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token", "refresh_token": "r", "expires_in": 3600,
		})
	})
	mux.HandleFunc("/", api)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	tokens := oauth.NewManager(oauth.Config{TokenURL: srv.URL + "/oauth/token", ClientID: "c", ClientSecret: "s"}, srv.Client())
	return New(srv.URL, tokens, srv.Client())
}

func TestFetchEventsSendsBearerAndNormalizes(t *testing.T) {
	var gotAuth atomic.Value
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"alerts": []map[string]any{{
			"alert_id": "a-9", "unit_ref": "u-2", "category": "smoke", "score": 60.0,
			"raised_at": "2026-09-21T11:59:40Z", "confirmed_at": "2026-09-21T12:00:00Z",
		}}})
	})
	evs, err := c.FetchEvents(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth.Load() != "Bearer test-token" {
		t.Fatalf("Authorization = %q", gotAuth.Load())
	}
	if len(evs) != 1 || evs[0].Event.Type != domain.HazardSmoke || evs[0].Event.Confidence != 0.6 {
		t.Fatalf("events = %+v", evs)
	}
}

func TestMalformedAlertIsSkippedNotFatal(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"alerts": []map[string]any{
			{"alert_id": "bad", "unit_ref": "u", "category": "meteor", "score": 10.0, "confirmed_at": "2026-09-21T12:00:00Z"},
			{"alert_id": "good", "unit_ref": "u", "category": "fire", "score": 90.0,
				"raised_at": "2026-09-21T11:59:40Z", "confirmed_at": "2026-09-21T12:00:00Z"},
		}})
	})
	evs, err := c.FetchEvents(context.Background(), now.Add(-time.Hour))
	if err == nil {
		t.Fatal("expected a reported error for the malformed alert")
	}
	if len(evs) != 1 || evs[0].Event.DedupKey != "good" {
		t.Fatalf("good alert should survive a bad sibling; got %+v", evs)
	}
}

func TestTransient500IsRetried(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"units": []map[string]any{{"unit_ref": "u1", "health": "green"}}})
	})
	devices, err := c.FetchDevices(context.Background())
	if err != nil {
		t.Fatalf("expected recovery after 500s, got %v", err)
	}
	if len(devices) != 1 || calls.Load() != 3 {
		t.Fatalf("devices=%d calls=%d", len(devices), calls.Load())
	}
}

func Test429IsRetried(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"units": []map[string]any{}})
	})
	if _, err := c.FetchDevices(context.Background()); err != nil {
		t.Fatalf("expected recovery after 429, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func Test404IsPermanentNoRetry(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	})
	if _, err := c.FetchDevices(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on 404)", calls.Load())
	}
}

func TestExpiredTokenServerSideIsRefreshedOnce(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		// First API call: pretend the token was revoked server-side.
		if calls.Add(1) == 1 {
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"units": []map[string]any{}})
	})
	if _, err := c.FetchDevices(context.Background()); err != nil {
		t.Fatalf("expected recovery via token refresh, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2 (one 401, one success with fresh token)", calls.Load())
	}
}

func TestSendCommandMapsVocabulary(t *testing.T) {
	type got struct {
		Instruction string          `json:"instruction"`
		RequestRef  string          `json:"request_ref"`
		Params      json.RawMessage `json:"params"`
	}
	var receivedPath atomic.Value
	var received atomic.Value
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var g got
		_ = json.NewDecoder(r.Body).Decode(&g)
		received.Store(g)
		receivedPath.Store(r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})
	cmd := domain.Command{ID: "cmd-1", Type: domain.CommandStartMonitoring}
	state, _, err := c.SendCommand(context.Background(), "u-7", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if state != domain.CommandDelivered {
		t.Fatalf("state = %v, want DELIVERED", state)
	}
	g := received.Load().(got)
	// Internal START_MONITORING must become AlphaSense's "arm".
	if g.Instruction != "arm" || g.RequestRef != "cmd-1" {
		t.Fatalf("wire command = %+v", g)
	}
	if receivedPath.Load() != "/api/v2/units/u-7/instructions" {
		t.Fatalf("path = %v", receivedPath.Load())
	}
}
