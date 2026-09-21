// Package alphasense is a SIMULATED third-party provider adapter. There is no
// real "AlphaSense" IoT vendor behind it; its API shape (see
// cmd/providersim) exists to prove the connector can integrate an
// OAuth2-authenticated, REST-polled provider whose wire format differs from
// the internal model.
//
// Everything AlphaSense-specific — the /api/v2 paths, 0–100 scores,
// green/amber/red health strings, "units" and "alerts" vocabulary — stays in
// this package. The rest of the connector sees only domain types.
package alphasense

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/oauth"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/reliability"
)

// Client is the adapter. It satisfies provider.DevicePoller,
// provider.EventPoller and provider.CommandSender.
type Client struct {
	baseURL string
	tokens  *oauth.Manager
	http    *http.Client
}

// New builds the adapter. httpClient may be nil.
func New(baseURL string, tokens *oauth.Manager, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), tokens: tokens, http: httpClient}
}

func (c *Client) Name() domain.ProviderName { return domain.ProviderAlphaSense }

// ---- Wire types (AlphaSense's schema, nobody else's) ---------------------

type wireUnit struct {
	UnitRef  string `json:"unit_ref"`
	Label    string `json:"label"`
	Health   string `json:"health"` // "green" | "amber" | "red"
	LastPing string `json:"last_ping"`
	Firmware string `json:"firmware"`
}

type wireAlert struct {
	AlertID   string  `json:"alert_id"`
	UnitRef   string  `json:"unit_ref"`
	Category  string  `json:"category"` // "fire" | "smoke"
	Score     float64 `json:"score"`    // 0–100, not 0–1
	RaisedAt  string  `json:"raised_at"`
	Confirmed string  `json:"confirmed_at"`
}

// ---- Normalization -------------------------------------------------------

// normalizeUnit maps an AlphaSense unit onto the internal Device model.
func normalizeUnit(u wireUnit, now time.Time) (domain.Device, error) {
	if u.UnitRef == "" {
		return domain.Device{}, fmt.Errorf("unit missing unit_ref")
	}
	status := domain.DeviceUnknown
	switch u.Health {
	case "green", "amber": // amber = degraded but reachable
		status = domain.DeviceOnline
	case "red":
		status = domain.DeviceOffline
	}
	lastSeen := time.Time{}
	if u.LastPing != "" {
		if t, err := time.Parse(time.RFC3339, u.LastPing); err == nil {
			lastSeen = t
		}
	}
	return domain.Device{
		ExternalID: u.UnitRef,
		Provider:   domain.ProviderAlphaSense,
		Name:       u.Label,
		Status:     status,
		AppVersion: u.Firmware,
		LastSeenAt: lastSeen,
		UpdatedAt:  now,
	}, nil
}

// normalizeAlert maps an AlphaSense alert onto the internal HazardEvent.
// AlphaSense scores are 0–100; internally confidence is [0,1].
func normalizeAlert(a wireAlert, received time.Time) (provider.NormalizedEvent, error) {
	var typ domain.HazardType
	switch a.Category {
	case "fire":
		typ = domain.HazardFire
	case "smoke":
		typ = domain.HazardSmoke
	default:
		return provider.NormalizedEvent{}, fmt.Errorf("unknown alert category %q", a.Category)
	}
	if a.AlertID == "" || a.UnitRef == "" {
		return provider.NormalizedEvent{}, fmt.Errorf("alert missing alert_id/unit_ref")
	}
	if a.Score < 0 || a.Score > 100 {
		return provider.NormalizedEvent{}, fmt.Errorf("alert score %v outside [0,100]", a.Score)
	}
	confirmed, err := time.Parse(time.RFC3339, a.Confirmed)
	if err != nil {
		return provider.NormalizedEvent{}, fmt.Errorf("bad confirmed_at: %w", err)
	}
	raised := confirmed
	if t, err := time.Parse(time.RFC3339, a.RaisedAt); err == nil {
		raised = t
	}
	ev := domain.HazardEvent{
		DedupKey:    a.AlertID,
		Source:      domain.ProviderAlphaSense,
		Type:        typ,
		Confidence:  a.Score / 100.0,
		FirstSeenAt: raised,
		ConfirmedAt: confirmed,
		ReceivedAt:  received,
	}
	return provider.NormalizedEvent{ExternalDeviceID: a.UnitRef, Event: ev}, nil
}

// ---- Provider capabilities ----------------------------------------------

// FetchDevices implements provider.DevicePoller.
func (c *Client) FetchDevices(ctx context.Context) ([]domain.Device, error) {
	var payload struct {
		Units []wireUnit `json:"units"`
	}
	if err := c.get(ctx, "/api/v2/units", &payload); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	devices := make([]domain.Device, 0, len(payload.Units))
	for _, u := range payload.Units {
		d, err := normalizeUnit(u, now)
		if err != nil {
			// One malformed unit must not sink the whole poll; skip it.
			continue
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// FetchEvents implements provider.EventPoller.
func (c *Client) FetchEvents(ctx context.Context, since time.Time) ([]provider.NormalizedEvent, error) {
	var payload struct {
		Alerts []wireAlert `json:"alerts"`
	}
	q := url.Values{"since": {since.UTC().Format(time.RFC3339)}}
	if err := c.get(ctx, "/api/v2/alerts?"+q.Encode(), &payload); err != nil {
		return nil, err
	}
	received := time.Now().UTC()
	events := make([]provider.NormalizedEvent, 0, len(payload.Alerts))
	var firstErr error
	for _, a := range payload.Alerts {
		ev, err := normalizeAlert(a, received)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // malformed alert: skipped, reported, not fatal
		}
		events = append(events, ev)
	}
	return events, firstErr
}

// SendCommand implements provider.CommandSender. AlphaSense's command
// vocabulary ("instructions") differs from the internal one; the mapping
// lives here and nowhere else.
func (c *Client) SendCommand(ctx context.Context, externalDeviceID string, cmd domain.Command) (domain.CommandState, string, error) {
	instruction := map[domain.CommandType]string{
		domain.CommandGetStatus:       "status_report",
		domain.CommandStartMonitoring: "arm",
		domain.CommandStopMonitoring:  "disarm",
		domain.CommandUpdateConfig:    "reconfigure",
	}[cmd.Type]
	if instruction == "" {
		return domain.CommandFailed, "", reliability.Permanent{Err: fmt.Errorf("alphasense cannot express command %q", cmd.Type)}
	}

	body, _ := json.Marshal(map[string]any{
		"instruction": instruction,
		"params":      json.RawMessage(orEmptyObject(cmd.Payload)),
		"request_ref": cmd.ID, // provider-side idempotency handle
	})
	path := "/api/v2/units/" + url.PathEscape(externalDeviceID) + "/instructions"
	if err := c.do(ctx, http.MethodPost, path, string(body), nil); err != nil {
		return domain.CommandFailed, "", err
	}
	// AlphaSense accepts instructions asynchronously (202); the connector
	// marks the command DELIVERED, not ACKNOWLEDGED.
	return domain.CommandDelivered, "accepted by alphasense", nil
}

func orEmptyObject(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

// ---- HTTP plumbing -------------------------------------------------------

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, "", out)
}

// do performs one authenticated request with retry/backoff. A 401 invalidates
// the cached token and retries once with a fresh one (revoked-token
// recovery). 4xx are permanent; 429 and 5xx are transient.
func (c *Client) do(ctx context.Context, method, path, body string, out any) error {
	refreshed := false
	return reliability.Do(ctx, reliability.DefaultPolicy(), func(ctx context.Context) error {
		token, err := c.tokens.AccessToken(ctx)
		if err != nil {
			return fmt.Errorf("alphasense auth: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(body))
		if err != nil {
			return reliability.Permanent{Err: err}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("alphasense %s %s: %w", method, path, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out != nil {
				if err := json.Unmarshal(data, out); err != nil {
					return reliability.Permanent{Err: fmt.Errorf("alphasense malformed response: %w", err)}
				}
			}
			return nil
		case resp.StatusCode == http.StatusUnauthorized:
			// Token revoked or expired server-side. Invalidate once and let
			// the retry loop fetch a fresh token; a second 401 is permanent.
			if !refreshed {
				refreshed = true
				c.tokens.Invalidate()
				return fmt.Errorf("alphasense 401, retrying with fresh token")
			}
			return reliability.Permanent{Err: fmt.Errorf("alphasense rejected fresh token (401)")}
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return fmt.Errorf("alphasense %s %s: status %d", method, path, resp.StatusCode)
		default:
			return reliability.Permanent{Err: fmt.Errorf("alphasense %s %s: status %d", method, path, resp.StatusCode)}
		}
	})
}
