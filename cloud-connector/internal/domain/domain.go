// Package domain holds the connector's internal model. Everything the rest of
// the service passes around is defined here, and nothing here refers to any
// provider's wire format: provider adapters translate *into* these types at
// the boundary and provider quirks are not allowed to leak past them.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// HazardType is the normalized hazard taxonomy. It mirrors the Android app's
// DetectorType enum (core/model/DetectorType.kt) so an edge event maps 1:1.
type HazardType string

const (
	HazardFire            HazardType = "FIRE"
	HazardSmoke           HazardType = "SMOKE"
	HazardFall            HazardType = "FALL"
	HazardIntrusion       HazardType = "INTRUSION"
	HazardCameraTampering HazardType = "CAMERA_TAMPERING"
)

// ValidHazardType reports whether t is a known hazard type.
func ValidHazardType(t HazardType) bool {
	switch t {
	case HazardFire, HazardSmoke, HazardFall, HazardIntrusion, HazardCameraTampering:
		return true
	}
	return false
}

// ProviderName identifies where a device or event came from.
type ProviderName string

const (
	// ProviderGuardianEdge is the first-party Android edge application.
	ProviderGuardianEdge ProviderName = "guardian-edge"
	// ProviderAlphaSense is a simulated third-party provider integrated via
	// OAuth2 + REST polling. It is not a real commercial provider.
	ProviderAlphaSense ProviderName = "alphasense"
	// ProviderBetaGrid is a simulated third-party provider integrated via
	// HMAC-signed webhooks. It is not a real commercial provider.
	ProviderBetaGrid ProviderName = "betagrid"
)

// ValidProvider reports whether p is a known provider.
func ValidProvider(p ProviderName) bool {
	switch p {
	case ProviderGuardianEdge, ProviderAlphaSense, ProviderBetaGrid:
		return true
	}
	return false
}

// DeviceStatus is the normalized device availability state. Providers report
// this in wildly different shapes (booleans, enums, "lastHeartbeat" ages);
// adapters must map them onto these three values.
type DeviceStatus string

const (
	DeviceOnline  DeviceStatus = "ONLINE"
	DeviceOffline DeviceStatus = "OFFLINE"
	DeviceUnknown DeviceStatus = "UNKNOWN"
)

// Device is a monitored edge device, whichever provider owns it.
type Device struct {
	// ID is the connector's own UUID for the device.
	ID string
	// ExternalID is the identifier the device/provider uses for itself.
	// Unique per provider, not globally.
	ExternalID string
	Provider   ProviderName
	Name       string
	Status     DeviceStatus
	// AppVersion is the edge application version, when the provider reports one.
	AppVersion string
	LastSeenAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// MonitoringStatus is the edge pipeline's coarse state, from telemetry.
type MonitoringStatus string

const (
	MonitoringIdle    MonitoringStatus = "IDLE"
	MonitoringActive  MonitoringStatus = "MONITORING"
	MonitoringUnknown MonitoringStatus = "UNKNOWN"
)

// TelemetryEvent is one normalized health snapshot from a device. Fields
// mirror the Android InferenceMetrics type plus identity/status; nothing here
// is personal data.
type TelemetryEvent struct {
	ID       string
	DeviceID string
	// ReportedAt is the device's own clock; ReceivedAt is the connector's.
	// Both are kept because edge clocks drift and events arrive late.
	ReportedAt         time.Time
	ReceivedAt         time.Time
	MonitoringStatus   MonitoringStatus
	ModelStatus        string
	InferenceLatencyMs int64
	ProcessedFPS       int32
	DroppedFrames      int64
	ConfirmedEvents    int64
	AppVersion         string
}

// HazardEvent is a normalized, confirmed hazard from any source. For edge
// events this corresponds 1:1 to the Android SecurityEvent.
type HazardEvent struct {
	// ID is the connector's UUID.
	ID string
	// DedupKey makes redelivery harmless: for edge events it is the
	// SecurityEvent id minted on-device; for webhook events it is the
	// provider's delivery/event id. Unique per (device, source).
	DedupKey    string
	DeviceID    string
	Source      ProviderName
	Type        HazardType
	Confidence  float64
	FirstSeenAt time.Time
	ConfirmedAt time.Time
	ReceivedAt  time.Time
}

// Validate checks the invariants every hazard event must satisfy regardless
// of source. Adapters call this after normalization; the ingest and webhook
// paths reject anything that fails it.
func (e HazardEvent) Validate() error {
	var errs []error
	if e.DedupKey == "" {
		errs = append(errs, errors.New("missing dedup key"))
	}
	if e.DeviceID == "" {
		errs = append(errs, errors.New("missing device id"))
	}
	if !ValidProvider(e.Source) {
		errs = append(errs, fmt.Errorf("unknown source %q", e.Source))
	}
	if !ValidHazardType(e.Type) {
		errs = append(errs, fmt.Errorf("unknown hazard type %q", e.Type))
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		errs = append(errs, fmt.Errorf("confidence %v outside [0,1]", e.Confidence))
	}
	if e.ConfirmedAt.IsZero() {
		errs = append(errs, errors.New("missing confirmed-at timestamp"))
	}
	return errors.Join(errs...)
}

// CommandType enumerates the safe, remote-operable commands. Nothing here can
// cause a physical action; the edge app remains the authority on what it
// actually executes.
type CommandType string

const (
	CommandGetStatus       CommandType = "GET_STATUS"
	CommandStartMonitoring CommandType = "START_MONITORING"
	CommandStopMonitoring  CommandType = "STOP_MONITORING"
	CommandUpdateConfig    CommandType = "UPDATE_MONITORING_CONFIG"
)

// ValidCommandType reports whether t is a known command type.
func ValidCommandType(t CommandType) bool {
	switch t {
	case CommandGetStatus, CommandStartMonitoring, CommandStopMonitoring, CommandUpdateConfig:
		return true
	}
	return false
}

// CommandState is the delivery lifecycle of a command.
//
//	PENDING ──(device fetches)──▶ DELIVERED ──(device reports)──▶ ACKNOWLEDGED
//	   │                              │
//	   └──────────(expiry)────────────┴──────▶ FAILED
//
// Transitions are enforced in the command service; anything else is rejected.
type CommandState string

const (
	CommandPending      CommandState = "PENDING"
	CommandDelivered    CommandState = "DELIVERED"
	CommandAcknowledged CommandState = "ACKNOWLEDGED"
	CommandFailed       CommandState = "FAILED"
)

// Command is a cloud→edge instruction.
type Command struct {
	ID       string
	DeviceID string
	Type     CommandType
	// Payload is a small JSON document for parameterized commands
	// (UPDATE_MONITORING_CONFIG); empty otherwise.
	Payload string
	// IdempotencyKey lets a caller retry SendCommand without enqueueing the
	// command twice.
	IdempotencyKey string
	State          CommandState
	// ResultMessage carries the device-reported outcome or the failure reason.
	ResultMessage string
	CreatedAt     time.Time
	DeliveredAt   *time.Time
	CompletedAt   *time.Time
	// ExpiresAt bounds how long an undelivered command stays PENDING before
	// the reaper fails it (edge devices can be offline for a long time).
	ExpiresAt time.Time
}

// CanTransition reports whether a command may move from -> to. This is the
// single source of truth for the state machine.
func CanTransition(from, to CommandState) bool {
	switch from {
	case CommandPending:
		return to == CommandDelivered || to == CommandFailed
	case CommandDelivered:
		return to == CommandAcknowledged || to == CommandFailed
	default:
		return false
	}
}

// ProviderConnection is the stored integration state for one external
// provider: where it lives and the credentials to talk to it. Token material
// is encrypted at rest by the storage layer; this in-memory form carries the
// plaintext only as far as the provider adapter that needs it.
type ProviderConnection struct {
	ID       string
	Provider ProviderName
	BaseURL  string
	// AccessToken / RefreshToken are OAuth2 material for polling providers,
	// empty for webhook providers.
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	// WebhookSecret is the shared HMAC secret for webhook providers, empty
	// for polling providers.
	WebhookSecret string
	Status        string
	UpdatedAt     time.Time
}
