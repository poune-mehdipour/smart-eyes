// Package betagrid is a SIMULATED third-party provider adapter. There is no
// real "BetaGrid" vendor; it exists to prove the connector can integrate a
// push (webhook) provider whose schema disagrees with the internal model in
// every way that hurts: different field names, unix-second timestamps,
// different hazard vocabulary ("FLAME", "SMOKE_DETECTED"), and a categorical
// certainty instead of a numeric confidence.
//
// This package owns parsing, HMAC signature verification and normalization
// for BetaGrid deliveries. The transport-level webhook handling (dedup,
// timestamp window, persistence) lives in internal/webhook and is
// provider-agnostic.
package betagrid

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
)

// SignatureHeader carries the hex HMAC-SHA256 of the raw request body.
const SignatureHeader = "X-BetaGrid-Signature"

// Adapter implements provider.Provider. BetaGrid pushes events, so it is
// deliberately *not* an EventPoller, and it accepts no commands — the
// capability simply isn't implemented, and the command dispatcher refuses
// commands to BetaGrid devices with a clear error instead of pretending.
type Adapter struct{}

func (Adapter) Name() domain.ProviderName { return domain.ProviderBetaGrid }

// ---- Wire types (BetaGrid's schema) --------------------------------------

// wireDelivery is one webhook POST body.
type wireDelivery struct {
	MsgID  string          `json:"msg_id"`
	SentAt int64           `json:"sent_at"` // unix seconds
	Kind   string          `json:"kind"`    // "hazard" | "heartbeat"
	Data   json.RawMessage `json:"data"`
}

type wireHazard struct {
	Device    string `json:"device"`
	Hazard    string `json:"hazard"`    // "FLAME" | "SMOKE_DETECTED"
	Certainty string `json:"certainty"` // "low" | "medium" | "high"
	Started   int64  `json:"started"`   // unix seconds
	Verified  int64  `json:"verified"`  // unix seconds
}

type wireHeartbeat struct {
	Device string `json:"device"`
	State  string `json:"state"` // "up" | "down"
}

// ---- Signature -----------------------------------------------------------

// VerifySignature checks the HMAC-SHA256 of the raw body against the header
// value using a constant-time comparison.
func VerifySignature(body []byte, headerValue, secret string) bool {
	if headerValue == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(headerValue)
	if err != nil {
		return false
	}
	return hmac.Equal(want, got)
}

// Sign computes the signature header value for a body. Used by the provider
// simulator and by tests; the connector itself only verifies.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---- Parsing & normalization ---------------------------------------------

// Delivery is a parsed, but not yet normalized, BetaGrid webhook.
type Delivery struct {
	MsgID  string
	SentAt time.Time
	// Event is non-nil for kind=hazard.
	Event *provider.NormalizedEvent
	// Heartbeat is non-nil for kind=heartbeat.
	Heartbeat *Heartbeat
}

// Heartbeat is a normalized device-status ping.
type Heartbeat struct {
	ExternalDeviceID string
	Status           domain.DeviceStatus
}

// certaintyToConfidence maps BetaGrid's categorical certainty onto the
// internal numeric confidence. The mapping is a documented convention, not a
// calibration: low/medium/high become 0.25/0.50/0.90.
func certaintyToConfidence(c string) (float64, error) {
	switch c {
	case "low":
		return 0.25, nil
	case "medium":
		return 0.50, nil
	case "high":
		return 0.90, nil
	default:
		return 0, fmt.Errorf("unknown certainty %q", c)
	}
}

// Parse validates and normalizes one webhook body. It rejects — never
// silently accepts — anything malformed: unknown kinds, unknown hazard or
// certainty vocabulary, missing identifiers, zero timestamps.
func Parse(body []byte, now time.Time) (Delivery, error) {
	var w wireDelivery
	if err := json.Unmarshal(body, &w); err != nil {
		return Delivery{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if w.MsgID == "" {
		return Delivery{}, fmt.Errorf("missing msg_id")
	}
	if w.SentAt <= 0 {
		return Delivery{}, fmt.Errorf("missing sent_at")
	}
	d := Delivery{MsgID: w.MsgID, SentAt: time.Unix(w.SentAt, 0).UTC()}

	switch w.Kind {
	case "hazard":
		var h wireHazard
		if err := json.Unmarshal(w.Data, &h); err != nil {
			return Delivery{}, fmt.Errorf("malformed hazard data: %w", err)
		}
		if h.Device == "" {
			return Delivery{}, fmt.Errorf("hazard missing device")
		}
		var typ domain.HazardType
		switch h.Hazard {
		case "FLAME":
			typ = domain.HazardFire
		case "SMOKE_DETECTED":
			typ = domain.HazardSmoke
		default:
			return Delivery{}, fmt.Errorf("unknown hazard %q", h.Hazard)
		}
		conf, err := certaintyToConfidence(h.Certainty)
		if err != nil {
			return Delivery{}, err
		}
		if h.Verified <= 0 {
			return Delivery{}, fmt.Errorf("hazard missing verified timestamp")
		}
		started := h.Started
		if started <= 0 {
			started = h.Verified
		}
		d.Event = &provider.NormalizedEvent{
			ExternalDeviceID: h.Device,
			Event: domain.HazardEvent{
				DedupKey:    w.MsgID,
				Source:      domain.ProviderBetaGrid,
				Type:        typ,
				Confidence:  conf,
				FirstSeenAt: time.Unix(started, 0).UTC(),
				ConfirmedAt: time.Unix(h.Verified, 0).UTC(),
				ReceivedAt:  now,
			},
		}
		return d, nil

	case "heartbeat":
		var hb wireHeartbeat
		if err := json.Unmarshal(w.Data, &hb); err != nil {
			return Delivery{}, fmt.Errorf("malformed heartbeat data: %w", err)
		}
		if hb.Device == "" {
			return Delivery{}, fmt.Errorf("heartbeat missing device")
		}
		status := domain.DeviceUnknown
		switch hb.State {
		case "up":
			status = domain.DeviceOnline
		case "down":
			status = domain.DeviceOffline
		}
		d.Heartbeat = &Heartbeat{ExternalDeviceID: hb.Device, Status: status}
		return d, nil

	default:
		return Delivery{}, fmt.Errorf("unknown kind %q", w.Kind)
	}
}
