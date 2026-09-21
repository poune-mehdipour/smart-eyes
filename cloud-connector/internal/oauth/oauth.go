// Package oauth implements the client side of an OAuth 2.0 token lifecycle
// for polling providers: obtain, cache, expire, refresh, and recover.
//
// The manager is deliberately hand-rolled rather than pulled from a library:
// the point of this package is that the lifecycle — expiry skew, single-flight
// refresh, invalid_grant recovery — is explicit and unit-tested.
//
// Grant model (matches the simulated AlphaSense provider, and several real
// IoT vendor APIs): a client_credentials grant issues an access token *and* a
// refresh token; refreshes use grant_type=refresh_token. If the refresh token
// is rejected (invalid_grant), the manager falls back to a fresh
// client_credentials grant instead of wedging.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/reliability"
)

// expirySkew is subtracted from the token lifetime so we refresh slightly
// early and never hand a caller a token that dies mid-request.
const expirySkew = 30 * time.Second

// Token is the provider-issued token material. It never appears in logs.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Valid reports whether the access token is usable at time now.
func (t Token) Valid(now time.Time) bool {
	return t.AccessToken != "" && now.Before(t.Expiry.Add(-expirySkew))
}

// Config identifies the token endpoint and client credentials.
type Config struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
}

// Manager caches a token and refreshes it on demand. Safe for concurrent
// use; concurrent callers during a refresh share one refresh request
// (single-flight) instead of stampeding the provider.
type Manager struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	token Token
}

// NewManager builds a Manager. client may be nil (http.DefaultClient is a
// poor choice for outbound calls, so a timeout-bounded client is created).
func NewManager(cfg Config, client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Manager{cfg: cfg, client: client, now: time.Now}
}

// AccessToken returns a currently-valid access token, performing the initial
// grant or a refresh if needed. The mutex is held across the network call on
// purpose: that is what makes concurrent callers single-flight.
func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token.Valid(m.now()) {
		return m.token.AccessToken, nil
	}

	tok, err := m.obtainLocked(ctx)
	if err != nil {
		return "", err
	}
	m.token = tok
	return tok.AccessToken, nil
}

// Invalidate drops the cached token. Callers use it when the provider
// answers 401 despite a token we believed valid (revocation, clock trouble):
// the next AccessToken call performs a fresh grant.
func (m *Manager) Invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = Token{}
}

// obtainLocked gets a new token: by refresh when we hold a refresh token,
// falling back to the initial grant when the refresh token is rejected.
func (m *Manager) obtainLocked(ctx context.Context) (Token, error) {
	if m.token.RefreshToken != "" {
		tok, err := m.grant(ctx, url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {m.token.RefreshToken},
		})
		if err == nil {
			return tok, nil
		}
		if !reliability.IsPermanent(err) {
			return Token{}, fmt.Errorf("refreshing token: %w", err)
		}
		// invalid_grant etc.: the refresh token is dead. Fall through to a
		// full re-auth rather than failing forever.
	}
	tok, err := m.grant(ctx, url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return Token{}, fmt.Errorf("obtaining token: %w", err)
	}
	return tok, nil
}

// tokenResponse is the wire form of the token endpoint's answer.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

// grant performs one token-endpoint request. 4xx answers are Permanent
// (retrying the same request cannot help); 5xx and transport errors are
// transient and retried with backoff.
func (m *Manager) grant(ctx context.Context, form url.Values) (Token, error) {
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)

	var tok Token
	err := reliability.Do(ctx, reliability.Policy{Attempts: 3, Base: 100 * time.Millisecond, Max: time.Second},
		func(ctx context.Context) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.TokenURL,
				strings.NewReader(form.Encode()))
			if err != nil {
				return reliability.Permanent{Err: err}
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := m.client.Do(req)
			if err != nil {
				return fmt.Errorf("token endpoint: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

			var tr tokenResponse
			switch {
			case resp.StatusCode == http.StatusOK:
				if err := json.Unmarshal(body, &tr); err != nil {
					return reliability.Permanent{Err: fmt.Errorf("malformed token response: %w", err)}
				}
				if tr.AccessToken == "" || tr.ExpiresIn <= 0 {
					return reliability.Permanent{Err: fmt.Errorf("token response missing access_token/expires_in")}
				}
				tok = Token{
					AccessToken:  tr.AccessToken,
					RefreshToken: tr.RefreshToken,
					Expiry:       m.now().Add(time.Duration(tr.ExpiresIn) * time.Second),
				}
				return nil
			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				_ = json.Unmarshal(body, &tr)
				// The OAuth error code (e.g. invalid_grant) is safe to log;
				// the request body with credentials is not, and never is.
				return reliability.Permanent{Err: fmt.Errorf("token endpoint %d (%s)", resp.StatusCode, tr.Error)}
			default:
				return fmt.Errorf("token endpoint %d", resp.StatusCode)
			}
		})
	if err != nil {
		return Token{}, err
	}
	return tok, nil
}

// current returns a copy of the cached token; used by tests only.
func (m *Manager) current() Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token
}
