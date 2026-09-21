package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
)

// These tests run against a real PostgreSQL named by TEST_DATABASE_URL
// (docker compose or CI provides one) and are skipped otherwise, so plain
// `go test ./...` stays hermetic. They verify what mocks cannot: that the
// unique constraints, the state-machine UPDATE guards, and FOR UPDATE SKIP
// LOCKED behave as designed in the actual database.

func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func mustDevice(t *testing.T, s *Store, ext string) string {
	t.Helper()
	id, err := s.UpsertDevice(context.Background(), domain.Device{
		ExternalID: ext, Provider: domain.ProviderGuardianEdge,
		Status: domain.DeviceOnline, LastSeenAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIntegrationDeviceUpsertIsStable(t *testing.T) {
	s := testStore(t)
	ext := "it-dev-" + uuid.NewString()
	id1 := mustDevice(t, s, ext)
	id2 := mustDevice(t, s, ext)
	if id1 != id2 {
		t.Fatalf("upsert minted a new id: %s vs %s", id1, id2)
	}
	d, err := s.FindDeviceByExternalID(context.Background(), domain.ProviderGuardianEdge, ext)
	if err != nil || d.ID != id1 {
		t.Fatalf("find: %+v, %v", d, err)
	}
}

func TestIntegrationHazardEventIdempotency(t *testing.T) {
	s := testStore(t)
	dev := mustDevice(t, s, "it-dev-"+uuid.NewString())
	ev := domain.HazardEvent{
		DedupKey: "it-evt-" + uuid.NewString(), DeviceID: dev,
		Source: domain.ProviderGuardianEdge, Type: domain.HazardFire, Confidence: 0.9,
		FirstSeenAt: time.Now().Add(-time.Second), ConfirmedAt: time.Now(), ReceivedAt: time.Now(),
	}
	if _, err := s.InsertHazardEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	_, err := s.InsertHazardEvent(context.Background(), ev)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second insert err = %v, want ErrDuplicate", err)
	}
}

func TestIntegrationWebhookDeliveryDedup(t *testing.T) {
	s := testStore(t)
	id := "it-del-" + uuid.NewString()
	if err := s.RecordWebhookDelivery(context.Background(), domain.ProviderBetaGrid, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWebhookDelivery(context.Background(), domain.ProviderBetaGrid, id); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestIntegrationCommandLifecycleAndGuards(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dev := mustDevice(t, s, "it-dev-"+uuid.NewString())

	cmd, err := s.CreateCommand(ctx, domain.Command{
		DeviceID: dev, Type: domain.CommandGetStatus, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.State != domain.CommandPending {
		t.Fatalf("state = %v", cmd.State)
	}

	// Illegal PENDING → ACKNOWLEDGED must be blocked by the SQL guard.
	if _, err := s.TransitionCommand(ctx, cmd.ID, domain.CommandAcknowledged, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}

	claimed, err := s.FetchPendingCommands(ctx, dev, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed = %v, err = %v", claimed, err)
	}
	// A second poll must not deliver the same command again.
	again, err := s.FetchPendingCommands(ctx, dev, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("second poll = %v, err = %v", again, err)
	}

	done, err := s.TransitionCommand(ctx, cmd.ID, domain.CommandAcknowledged, "did it")
	if err != nil || done.State != domain.CommandAcknowledged || done.ResultMessage != "did it" {
		t.Fatalf("final = %+v, err = %v", done, err)
	}
}

func TestIntegrationCommandIdempotencyKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dev := mustDevice(t, s, "it-dev-"+uuid.NewString())
	key := "it-idem-" + uuid.NewString()

	first, err := s.CreateCommand(ctx, domain.Command{
		DeviceID: dev, Type: domain.CommandStartMonitoring, IdempotencyKey: key,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateCommand(ctx, domain.Command{
		DeviceID: dev, Type: domain.CommandStartMonitoring, IdempotencyKey: key,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, ErrDuplicate) || second.ID != first.ID {
		t.Fatalf("second = %+v, err = %v; want original command + ErrDuplicate", second, err)
	}
}

func TestIntegrationCommandExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dev := mustDevice(t, s, "it-dev-"+uuid.NewString())
	cmd, err := s.CreateCommand(ctx, domain.Command{
		DeviceID: dev, Type: domain.CommandGetStatus, ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExpireCommands(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCommand(ctx, cmd.ID)
	if err != nil || got.State != domain.CommandFailed {
		t.Fatalf("expired command = %+v, err = %v", got, err)
	}
}

func TestIntegrationProviderConnectionTokenEncryption(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	cipher, err := NewTokenCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	pc := domain.ProviderConnection{
		Provider: domain.ProviderAlphaSense, BaseURL: "https://sim.example",
		AccessToken: "at-secret", RefreshToken: "rt-secret",
		TokenExpiry: time.Now().Add(time.Hour),
	}
	if err := s.SaveProviderConnection(ctx, pc, cipher); err != nil {
		t.Fatal(err)
	}

	// Raw row must not contain the plaintext.
	var raw []byte
	err = s.pool.QueryRow(ctx,
		`SELECT access_token_enc FROM provider_connections WHERE provider = $1`,
		domain.ProviderAlphaSense).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "at-secret" || len(raw) == 0 {
		t.Fatal("access token stored in plaintext (or not at all)")
	}

	got, err := s.GetProviderConnection(ctx, domain.ProviderAlphaSense, cipher)
	if err != nil || got.AccessToken != "at-secret" || got.RefreshToken != "rt-secret" {
		t.Fatalf("round trip = %+v, err = %v", got, err)
	}
}

func TestIntegrationTelemetryLatest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dev := mustDevice(t, s, "it-dev-"+uuid.NewString())
	for i, ts := range []time.Time{time.Now().Add(-2 * time.Minute), time.Now()} {
		err := s.InsertTelemetry(ctx, domain.TelemetryEvent{
			DeviceID: dev, ReportedAt: ts, ReceivedAt: time.Now(),
			MonitoringStatus: domain.MonitoringActive, ProcessedFPS: int32(i + 1),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, err := s.LatestTelemetry(ctx, dev)
	if err != nil || latest.ProcessedFPS != 2 {
		t.Fatalf("latest = %+v, err = %v", latest, err)
	}
}
