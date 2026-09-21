package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIdP is a scriptable token endpoint.
type fakeIdP struct {
	mu            sync.Mutex
	grants        []string // grant_type of each request, in order
	fail          int      // next N requests answer failStatus
	failCode      int
	ttl           int64
	rejectRefresh bool // answer invalid_grant to refresh_token grants
	seq           int
}

func (f *fakeIdP) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		grant := r.PostForm.Get("grant_type")
		f.grants = append(f.grants, grant)

		if f.fail > 0 {
			f.fail--
			http.Error(w, `{"error":"server_error"}`, f.failCode)
			return
		}
		if grant == "refresh_token" && f.rejectRefresh {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		f.seq++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-" + strconv.Itoa(f.seq),
			"refresh_token": "refresh-" + strconv.Itoa(f.seq),
			"expires_in":    f.ttl,
		})
	}
}

func newManagerForTest(t *testing.T, idp *fakeIdP) (*Manager, *httptest.Server, *time.Time) {
	t.Helper()
	if idp.ttl == 0 {
		idp.ttl = 3600
	}
	if idp.failCode == 0 {
		idp.failCode = http.StatusInternalServerError
	}
	srv := httptest.NewServer(idp.handler(t))
	t.Cleanup(srv.Close)
	m := NewManager(Config{TokenURL: srv.URL, ClientID: "id", ClientSecret: "secret"}, srv.Client())
	now := time.Now()
	m.now = func() time.Time { return now }
	return m, srv, &now
}

func (f *fakeIdP) grantLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.grants...)
}

func TestInitialGrantAndCaching(t *testing.T) {
	idp := &fakeIdP{}
	m, _, _ := newManagerForTest(t, idp)

	tok1, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok1 != tok2 {
		t.Fatalf("expected cached token, got %q then %q", tok1, tok2)
	}
	if got := idp.grantLog(); len(got) != 1 || got[0] != "client_credentials" {
		t.Fatalf("grants = %v, want one client_credentials", got)
	}
}

func TestExpiryTriggersRefreshGrant(t *testing.T) {
	idp := &fakeIdP{ttl: 60}
	m, _, now := newManagerForTest(t, idp)

	tok1, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Advance past expiry (60s TTL, 30s skew): token must be considered dead.
	*now = now.Add(45 * time.Second)
	tok2, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == tok2 {
		t.Fatal("expected a new access token after expiry")
	}
	if got := idp.grantLog(); len(got) != 2 || got[1] != "refresh_token" {
		t.Fatalf("grants = %v, want [client_credentials refresh_token]", got)
	}
}

func TestRefreshSkewRefreshesEarly(t *testing.T) {
	idp := &fakeIdP{ttl: 60}
	m, _, now := newManagerForTest(t, idp)
	if _, err := m.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 35s into a 60s token: inside the 30s skew window → refresh early.
	*now = now.Add(35 * time.Second)
	if _, err := m.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := idp.grantLog(); len(got) != 2 {
		t.Fatalf("grants = %v, want a refresh before hard expiry", got)
	}
}

func TestInvalidRefreshFallsBackToClientCredentials(t *testing.T) {
	idp := &fakeIdP{ttl: 60, rejectRefresh: true}
	m, _, now := newManagerForTest(t, idp)
	if _, err := m.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	tok, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("expected recovery via client_credentials, got %v", err)
	}
	if tok == "" {
		t.Fatal("empty token after recovery")
	}
	got := idp.grantLog()
	if len(got) != 3 || got[1] != "refresh_token" || got[2] != "client_credentials" {
		t.Fatalf("grants = %v, want [client_credentials refresh_token client_credentials]", got)
	}
}

func TestTransientTokenEndpointFailureIsRetried(t *testing.T) {
	idp := &fakeIdP{fail: 2, failCode: http.StatusInternalServerError}
	m, _, _ := newManagerForTest(t, idp)
	tok, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if got := idp.grantLog(); len(got) != 3 {
		t.Fatalf("grants = %v, want 3 attempts", got)
	}
}

func TestPermanentClientErrorFailsWithoutRetry(t *testing.T) {
	idp := &fakeIdP{fail: 99, failCode: http.StatusUnauthorized}
	m, _, _ := newManagerForTest(t, idp)
	if _, err := m.AccessToken(context.Background()); err == nil {
		t.Fatal("expected error for invalid_client")
	}
	if got := idp.grantLog(); len(got) != 1 {
		t.Fatalf("grants = %v, want exactly 1 attempt (no retry on 401)", got)
	}
}

func TestInvalidateForcesNewGrant(t *testing.T) {
	idp := &fakeIdP{}
	m, _, _ := newManagerForTest(t, idp)
	tok1, _ := m.AccessToken(context.Background())
	m.Invalidate()
	tok2, err := m.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == tok2 {
		t.Fatal("expected a fresh token after Invalidate")
	}
}

func TestConcurrentCallersSingleFlight(t *testing.T) {
	idp := &fakeIdP{}
	m, _, _ := newManagerForTest(t, idp)
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.AccessToken(context.Background()); err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d concurrent callers failed", failures.Load())
	}
	if got := idp.grantLog(); len(got) != 1 {
		t.Fatalf("grants = %v, want exactly 1 (single-flight)", got)
	}
}
