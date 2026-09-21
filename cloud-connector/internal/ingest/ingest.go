// Package ingest is the REST API Guardian edge devices talk to. This is the
// edge↔cloud wire contract; the Kotlin side (`:core:cloud`,
// CloudContract.kt) mirrors these JSON shapes field for field.
//
// Design notes:
//   - Timestamps cross the wire as unix milliseconds because that is what the
//     Android pipeline already uses (SecurityEvent.confirmedAtMs etc.).
//   - Devices self-register on first contact: a fleet of phones should not
//     need a provisioning step to start reporting.
//   - POST /events answers 202 for duplicates too — from the device's point
//     of view a redelivered event *is* delivered; the response body says
//     "duplicate": true so the client can stop retrying that event.
package ingest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/commands"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// Store is the storage slice this package needs.
type Store interface {
	UpsertDevice(ctx context.Context, d domain.Device) (string, error)
	InsertTelemetry(ctx context.Context, t domain.TelemetryEvent) error
	FindDeviceByExternalID(ctx context.Context, p domain.ProviderName, externalID string) (domain.Device, error)
}

// API serves the edge endpoints.
type API struct {
	store    Store
	events   *events.Service
	commands *commands.Service
	log      *slog.Logger
	metrics  *observability.Metrics
	token    string
	maxBytes int64
}

// New builds the API. token == "" disables auth (development only; main logs
// a loud warning).
func New(store Store, ev *events.Service, cs *commands.Service, log *slog.Logger,
	m *observability.Metrics, token string, maxBytes int64) *API {
	return &API{store: store, events: ev, commands: cs, log: log, metrics: m, token: token, maxBytes: maxBytes}
}

// Register mounts the edge routes on mux, each wrapped with middleware.
func (a *API) Register(mux *http.ServeMux, wrap func(h http.Handler, route string) http.Handler) {
	mux.Handle("POST /v1/edge/telemetry", wrap(a.auth(a.postTelemetry), "edge_telemetry"))
	mux.Handle("POST /v1/edge/events", wrap(a.auth(a.postEvent), "edge_events"))
	mux.Handle("GET /v1/edge/commands", wrap(a.auth(a.getCommands), "edge_commands_poll"))
	mux.Handle("POST /v1/edge/commands/{id}/result", wrap(a.auth(a.postCommandResult), "edge_command_result"))
}

// ---- Wire types ----------------------------------------------------------

type telemetryRequest struct {
	DeviceID           string `json:"deviceId"`
	ReportedAtMs       int64  `json:"reportedAtMs"`
	MonitoringStatus   string `json:"monitoringStatus"`
	ModelStatus        string `json:"modelStatus"`
	InferenceLatencyMs int64  `json:"inferenceLatencyMs"`
	ProcessedFps       int32  `json:"processedFps"`
	DroppedFrames      int64  `json:"droppedFrames"`
	ConfirmedEvents    int64  `json:"confirmedEvents"`
	AppVersion         string `json:"appVersion"`
}

type eventRequest struct {
	DeviceID      string  `json:"deviceId"`
	EventID       string  `json:"eventId"`
	Type          string  `json:"type"`
	Confidence    float64 `json:"confidence"`
	FirstSeenMs   int64   `json:"firstSeenMs"`
	ConfirmedAtMs int64   `json:"confirmedAtMs"`
	AppVersion    string  `json:"appVersion"`
}

type commandWire struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

type commandResultRequest struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ---- Handlers ------------------------------------------------------------

func (a *API) postTelemetry(w http.ResponseWriter, r *http.Request) {
	var req telemetryRequest
	if !a.decode(w, r, &req) {
		return
	}
	if req.DeviceID == "" || req.ReportedAtMs <= 0 {
		badRequest(w, "deviceId and reportedAtMs are required")
		return
	}
	status := domain.MonitoringUnknown
	switch req.MonitoringStatus {
	case string(domain.MonitoringIdle):
		status = domain.MonitoringIdle
	case string(domain.MonitoringActive):
		status = domain.MonitoringActive
	}

	now := time.Now().UTC()
	deviceID, err := a.store.UpsertDevice(r.Context(), domain.Device{
		ExternalID: req.DeviceID,
		Provider:   domain.ProviderGuardianEdge,
		Status:     domain.DeviceOnline,
		AppVersion: req.AppVersion,
		LastSeenAt: now,
	})
	if err != nil {
		internalError(w, a.log, r, err)
		return
	}
	err = a.store.InsertTelemetry(r.Context(), domain.TelemetryEvent{
		DeviceID:           deviceID,
		ReportedAt:         time.UnixMilli(req.ReportedAtMs).UTC(),
		ReceivedAt:         now,
		MonitoringStatus:   status,
		ModelStatus:        req.ModelStatus,
		InferenceLatencyMs: req.InferenceLatencyMs,
		ProcessedFPS:       req.ProcessedFps,
		DroppedFrames:      req.DroppedFrames,
		ConfirmedEvents:    req.ConfirmedEvents,
		AppVersion:         req.AppVersion,
	})
	if err != nil {
		internalError(w, a.log, r, err)
		return
	}
	a.metrics.TelemetryReceived.Inc()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (a *API) postEvent(w http.ResponseWriter, r *http.Request) {
	var req eventRequest
	if !a.decode(w, r, &req) {
		return
	}
	if req.DeviceID == "" {
		badRequest(w, "deviceId is required")
		return
	}
	now := time.Now().UTC()
	deviceID, err := a.store.UpsertDevice(r.Context(), domain.Device{
		ExternalID: req.DeviceID,
		Provider:   domain.ProviderGuardianEdge,
		Status:     domain.DeviceOnline,
		AppVersion: req.AppVersion,
		LastSeenAt: now,
	})
	if err != nil {
		internalError(w, a.log, r, err)
		return
	}

	// A missing/zero epoch-ms field must map to the zero time so that
	// domain validation catches it (time.UnixMilli(0) is 1970, not zero).
	msToTime := func(ms int64) time.Time {
		if ms <= 0 {
			return time.Time{}
		}
		return time.UnixMilli(ms).UTC()
	}
	duplicate, err := a.events.Accept(r.Context(), domain.HazardEvent{
		DedupKey:    req.EventID,
		DeviceID:    deviceID,
		Source:      domain.ProviderGuardianEdge,
		Type:        domain.HazardType(req.Type),
		Confidence:  req.Confidence,
		FirstSeenAt: msToTime(req.FirstSeenMs),
		ConfirmedAt: msToTime(req.ConfirmedAtMs),
		ReceivedAt:  now,
	})
	if err != nil {
		var rej events.ErrRejected
		if errors.As(err, &rej) {
			badRequest(w, rej.Error())
			return
		}
		internalError(w, a.log, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "duplicate": duplicate})
}

func (a *API) getCommands(w http.ResponseWriter, r *http.Request) {
	externalID := r.URL.Query().Get("deviceId")
	if externalID == "" {
		badRequest(w, "deviceId query parameter is required")
		return
	}
	device, err := a.store.FindDeviceByExternalID(r.Context(), domain.ProviderGuardianEdge, externalID)
	if errors.Is(err, storage.ErrNotFound) {
		// Unknown device has no commands; not an error (it may simply not
		// have reported telemetry yet).
		writeJSON(w, http.StatusOK, map[string]any{"commands": []commandWire{}})
		return
	}
	if err != nil {
		internalError(w, a.log, r, err)
		return
	}
	cmds, err := a.commands.Poll(r.Context(), device.ID, 10)
	if err != nil {
		internalError(w, a.log, r, err)
		return
	}
	out := make([]commandWire, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, commandWire{ID: c.ID, Type: string(c.Type), Payload: c.Payload})
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": out})
}

func (a *API) postCommandResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req commandResultRequest
	if !a.decode(w, r, &req) {
		return
	}
	_, err := a.commands.ReportResult(r.Context(), id, req.OK, req.Message)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		http.Error(w, "unknown command", http.StatusNotFound)
	case errors.Is(err, storage.ErrInvalidTransition):
		http.Error(w, "command is not awaiting a result", http.StatusConflict)
	case err != nil:
		internalError(w, a.log, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "recorded"})
	}
}

// ---- Plumbing ------------------------------------------------------------

// auth enforces the edge bearer token with a constant-time comparison.
func (a *API) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.token != "" {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	})
}

// decode parses a bounded JSON body, rejecting unknown garbage loudly.
func (a *API) decode(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, a.maxBytes+1))
	if err != nil {
		badRequest(w, "unreadable body")
		return false
	}
	if int64(len(body)) > a.maxBytes {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return false
	}
	if err := json.Unmarshal(body, into); err != nil {
		badRequest(w, "malformed JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusBadRequest)
}

func internalError(w http.ResponseWriter, log *slog.Logger, r *http.Request, err error) {
	observability.LoggerFrom(r.Context(), log).Error("internal_error",
		"route", r.URL.Path, "error", err.Error())
	http.Error(w, "internal error", http.StatusInternalServerError)
}
