package betagrid

import (
	"strings"
	"testing"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
)

var now = time.Unix(1_758_441_600, 0).UTC()

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"msg_id":"m1"}`)
	secret := "topsecret"
	sig := Sign(body, secret)

	tests := []struct {
		name   string
		body   []byte
		header string
		secret string
		want   bool
	}{
		{"valid", body, sig, secret, true},
		{"wrong secret", body, sig, "othersecret", false},
		{"tampered body", []byte(`{"msg_id":"m2"}`), sig, secret, false},
		{"empty header", body, "", secret, false},
		{"non-hex header", body, "zzzz", secret, false},
		{"no secret configured", body, sig, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifySignature(tt.body, tt.header, tt.secret); got != tt.want {
				t.Fatalf("VerifySignature = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHazard(t *testing.T) {
	body := []byte(`{
		"msg_id": "bg-msg-1", "sent_at": 1758441600, "kind": "hazard",
		"data": {"device": "bg-unit-9", "hazard": "FLAME", "certainty": "high",
		         "started": 1758441580, "verified": 1758441590}
	}`)
	d, err := Parse(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.MsgID != "bg-msg-1" || d.Event == nil || d.Heartbeat != nil {
		t.Fatalf("unexpected delivery %+v", d)
	}
	e := d.Event.Event
	if d.Event.ExternalDeviceID != "bg-unit-9" {
		t.Fatalf("device = %q", d.Event.ExternalDeviceID)
	}
	// The whole point of normalization: FLAME → FIRE, "high" → 0.9,
	// unix seconds → time.Time, msg_id → dedup key.
	if e.Type != domain.HazardFire {
		t.Fatalf("type = %v, want FIRE", e.Type)
	}
	if e.Confidence != 0.90 {
		t.Fatalf("confidence = %v, want 0.90", e.Confidence)
	}
	if e.DedupKey != "bg-msg-1" || e.Source != domain.ProviderBetaGrid {
		t.Fatalf("dedup/source wrong: %+v", e)
	}
	if !e.FirstSeenAt.Equal(time.Unix(1758441580, 0)) || !e.ConfirmedAt.Equal(time.Unix(1758441590, 0)) {
		t.Fatalf("timestamps wrong: %+v", e)
	}
	if err := e.Validate(); !strings.Contains(err.Error(), "missing device id") {
		// DeviceID is resolved later; everything else must already be valid.
		t.Fatalf("expected only missing-device-id, got %v", err)
	}
}

func TestParseHeartbeat(t *testing.T) {
	body := []byte(`{"msg_id":"bg-hb-1","sent_at":1758441600,"kind":"heartbeat",
		"data":{"device":"bg-unit-9","state":"down"}}`)
	d, err := Parse(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Heartbeat == nil || d.Event != nil {
		t.Fatalf("unexpected delivery %+v", d)
	}
	if d.Heartbeat.Status != domain.DeviceOffline {
		t.Fatalf("status = %v, want OFFLINE", d.Heartbeat.Status)
	}
}

func TestParseCertaintyMapping(t *testing.T) {
	for certainty, want := range map[string]float64{"low": 0.25, "medium": 0.50, "high": 0.90} {
		body := []byte(`{"msg_id":"m","sent_at":1758441600,"kind":"hazard",
			"data":{"device":"d","hazard":"SMOKE_DETECTED","certainty":"` + certainty + `","verified":1758441590}}`)
		d, err := Parse(body, now)
		if err != nil {
			t.Fatalf("%s: %v", certainty, err)
		}
		if d.Event.Event.Confidence != want {
			t.Fatalf("%s → %v, want %v", certainty, d.Event.Event.Confidence, want)
		}
		if d.Event.Event.Type != domain.HazardSmoke {
			t.Fatalf("SMOKE_DETECTED → %v, want SMOKE", d.Event.Event.Type)
		}
	}
}

func TestParseMalformed(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `not json at all`},
		{"missing msg_id", `{"sent_at":1,"kind":"hazard","data":{}}`},
		{"missing sent_at", `{"msg_id":"m","kind":"hazard","data":{}}`},
		{"unknown kind", `{"msg_id":"m","sent_at":1,"kind":"telepathy","data":{}}`},
		{"unknown hazard", `{"msg_id":"m","sent_at":1,"kind":"hazard","data":{"device":"d","hazard":"GHOST","certainty":"high","verified":1}}`},
		{"unknown certainty", `{"msg_id":"m","sent_at":1,"kind":"hazard","data":{"device":"d","hazard":"FLAME","certainty":"sure","verified":1}}`},
		{"missing device", `{"msg_id":"m","sent_at":1,"kind":"hazard","data":{"hazard":"FLAME","certainty":"high","verified":1}}`},
		{"missing verified", `{"msg_id":"m","sent_at":1,"kind":"hazard","data":{"device":"d","hazard":"FLAME","certainty":"high"}}`},
		{"hazard data wrong type", `{"msg_id":"m","sent_at":1,"kind":"hazard","data":"nope"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.body), now); err == nil {
				t.Fatal("expected parse error, got nil")
			}
		})
	}
}
