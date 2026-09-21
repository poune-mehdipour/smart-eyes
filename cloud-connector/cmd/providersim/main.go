// Command providersim simulates the two external providers the connector
// integrates, so the full system runs end-to-end locally with no real vendor
// account:
//
//   - AlphaSense (polled): an OAuth2 token endpoint issuing short-lived
//     access tokens plus refresh tokens, and a REST API (/api/v2/units,
//     /api/v2/alerts, /api/v2/units/{ref}/instructions) that requires them.
//     Short token TTLs force the connector's refresh path to actually run.
//   - BetaGrid (push): a loop that POSTs HMAC-signed webhook deliveries
//     (hazards + heartbeats) to the connector, deliberately re-sending some
//     deliveries so the connector's idempotency is exercised for real.
//
// This binary is a test fixture, not part of the product: it fabricates
// alerts *inside an explicitly simulated provider* so the integration can be
// demonstrated. The connector treats it exactly like a real vendor API.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider/betagrid"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := env("SIM_ADDR", ":9000")

	as := &alphaSim{
		clientID:     env("SIM_CLIENT_ID", "alphasense-demo-client"),
		clientSecret: env("SIM_CLIENT_SECRET", "alphasense-demo-secret"),
		tokenTTL:     mustDur(env("SIM_TOKEN_TTL", "90s")),
		accessTokens: map[string]time.Time{},
		refreshToks:  map[string]bool{},
		log:          log,
	}
	as.seedAlerts()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", as.token)
	mux.HandleFunc("GET /api/v2/units", as.auth(as.units))
	mux.HandleFunc("GET /api/v2/alerts", as.auth(as.listAlerts))
	mux.HandleFunc("POST /api/v2/units/{ref}/instructions", as.auth(as.instructions))

	if target := os.Getenv("BETAGRID_TARGET_URL"); target != "" {
		secret := os.Getenv("BETAGRID_WEBHOOK_SECRET")
		if secret == "" {
			log.Error("BETAGRID_TARGET_URL set but BETAGRID_WEBHOOK_SECRET empty; not pushing")
		} else {
			interval := mustDur(env("BETAGRID_PUSH_INTERVAL", "45s"))
			go pushBetaGrid(log, target, secret, interval)
		}
	}

	log.Info("providersim_listening", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("providersim_failed", "error", err.Error())
		os.Exit(1)
	}
}

func mustDur(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 60 * time.Second
	}
	return d
}

// ---- AlphaSense simulation -------------------------------------------------

type alphaSim struct {
	clientID     string
	clientSecret string
	tokenTTL     time.Duration
	log          *slog.Logger

	mu           sync.Mutex
	accessTokens map[string]time.Time // token -> expiry
	refreshToks  map[string]bool
	alerts       []map[string]any
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *alphaSim) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("client_id") != a.clientID || r.PostForm.Get("client_secret") != a.clientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	switch r.PostForm.Get("grant_type") {
	case "client_credentials":
		// fine
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		if !a.refreshToks[rt] {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		delete(a.refreshToks, rt) // refresh tokens are single-use here
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		return
	}

	access, refresh := randomToken(), randomToken()
	a.accessTokens[access] = time.Now().Add(a.tokenTTL)
	a.refreshToks[refresh] = true
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    int(a.tokenTTL.Seconds()),
	})
}

func (a *alphaSim) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		a.mu.Lock()
		exp, ok := a.accessTokens[tok]
		a.mu.Unlock()
		if !ok || time.Now().After(exp) {
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *alphaSim) units(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"units": []map[string]any{
			{"unit_ref": "as-unit-01", "label": "Warehouse north cam", "health": "green",
				"last_ping": time.Now().UTC().Format(time.RFC3339), "firmware": "2.3.1"},
			{"unit_ref": "as-unit-02", "label": "Loading dock cam", "health": "amber",
				"last_ping": time.Now().Add(-90 * time.Second).UTC().Format(time.RFC3339), "firmware": "2.2.9"},
			{"unit_ref": "as-unit-03", "label": "Boiler room cam", "health": "red",
				"last_ping": time.Now().Add(-45 * time.Minute).UTC().Format(time.RFC3339), "firmware": "2.3.1"},
		},
	})
}

// seedAlerts creates a fresh alert every couple of minutes so pollers always
// have something recent to pick up.
func (a *alphaSim) seedAlerts() {
	makeAlert := func() map[string]any {
		n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
		cat := "smoke"
		if n.Int64()%2 == 0 {
			cat = "fire"
		}
		now := time.Now().UTC()
		return map[string]any{
			"alert_id":     fmt.Sprintf("as-alert-%d", n.Int64()),
			"unit_ref":     "as-unit-01",
			"category":     cat,
			"score":        55 + float64(n.Int64()%45),
			"raised_at":    now.Add(-20 * time.Second).Format(time.RFC3339),
			"confirmed_at": now.Format(time.RFC3339),
		}
	}
	a.alerts = []map[string]any{makeAlert()}
	go func() {
		for range time.Tick(2 * time.Minute) {
			a.mu.Lock()
			a.alerts = append(a.alerts, makeAlert())
			if len(a.alerts) > 50 {
				a.alerts = a.alerts[len(a.alerts)-50:]
			}
			a.mu.Unlock()
		}
	}()
}

func (a *alphaSim) listAlerts(w http.ResponseWriter, r *http.Request) {
	since, _ := time.Parse(time.RFC3339, r.URL.Query().Get("since"))
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]map[string]any, 0)
	for _, al := range a.alerts {
		if ts, err := time.Parse(time.RFC3339, al["confirmed_at"].(string)); err == nil && ts.After(since) {
			out = append(out, al)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"alerts": out})
}

func (a *alphaSim) instructions(w http.ResponseWriter, r *http.Request) {
	a.log.Info("alphasense_instruction_received", "unit", r.PathValue("ref"))
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"ticket": randomToken()[:8]})
}

// ---- BetaGrid webhook pusher ------------------------------------------------

func pushBetaGrid(log *slog.Logger, target, secret string, interval time.Duration) {
	client := &http.Client{Timeout: 10 * time.Second}
	seq := 0
	send := func(body []byte) {
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(betagrid.SignatureHeader, betagrid.Sign(body, secret))
		resp, err := client.Do(req)
		if err != nil {
			log.Warn("betagrid_push_failed", "error", err.Error())
			return
		}
		defer resp.Body.Close()
		log.Info("betagrid_pushed", "status", resp.StatusCode)
	}

	for range time.Tick(interval) {
		seq++
		now := time.Now().UTC()
		var payload map[string]any
		if seq%3 == 0 {
			payload = map[string]any{
				"msg_id": fmt.Sprintf("bg-msg-%d", seq), "sent_at": now.Unix(), "kind": "heartbeat",
				"data": map[string]any{"device": "bg-sensor-7", "state": "up"},
			}
		} else {
			hazard := "SMOKE_DETECTED"
			if seq%2 == 0 {
				hazard = "FLAME"
			}
			payload = map[string]any{
				"msg_id": fmt.Sprintf("bg-msg-%d", seq), "sent_at": now.Unix(), "kind": "hazard",
				"data": map[string]any{
					"device": "bg-sensor-7", "hazard": hazard, "certainty": "high",
					"started": now.Add(-15 * time.Second).Unix(), "verified": now.Unix(),
				},
			}
		}
		body, _ := json.Marshal(payload)
		send(body)
		if seq%4 == 0 {
			// Deliberate duplicate delivery: real webhook senders retry, and
			// the connector must treat the repeat as a no-op success.
			send(body)
		}
	}
}
