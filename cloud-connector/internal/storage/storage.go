// Package storage is the connector's only doorway to PostgreSQL. All SQL is
// parameterized (pgx sends queries and arguments separately — string
// concatenation never builds a statement), all idempotency lives in unique
// constraints, and migrations are embedded in the binary and applied at
// startup so the schema can never drift from the code that expects it.
package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/migrations"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when an insert was suppressed by an idempotency
// constraint. Callers treat it as success-with-a-metric, not a failure.
var ErrDuplicate = errors.New("duplicate")

// ErrInvalidTransition is returned when a command state change violates the
// state machine.
var ErrInvalidTransition = errors.New("invalid command state transition")

// Store wraps a pgx pool with the connector's queries.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects, verifies the connection, and applies pending migrations.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrating: %w", err)
	}
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping verifies database reachability; it backs the readiness endpoint.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// migrate applies embedded migrations in filename order, recording each in
// schema_migrations. Each migration runs in its own transaction.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("applying %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ---- Devices ---------------------------------------------------------------

// UpsertDevice creates or refreshes a device keyed by (provider, external_id)
// and returns its connector UUID. Zero-value fields never clobber known data.
func (s *Store) UpsertDevice(ctx context.Context, d domain.Device) (string, error) {
	id := uuid.NewString()
	var got string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO devices (id, provider, external_id, name, status, app_version, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '0001-01-01 00:00:00+00'::timestamptz), now(), now())
		ON CONFLICT (provider, external_id) DO UPDATE SET
			name         = COALESCE(NULLIF(EXCLUDED.name, ''), devices.name),
			status       = CASE WHEN EXCLUDED.status = 'UNKNOWN' THEN devices.status ELSE EXCLUDED.status END,
			app_version  = COALESCE(NULLIF(EXCLUDED.app_version, ''), devices.app_version),
			last_seen_at = GREATEST(COALESCE(EXCLUDED.last_seen_at, devices.last_seen_at), devices.last_seen_at),
			updated_at   = now()
		RETURNING id`,
		id, d.Provider, d.ExternalID, d.Name, d.Status, d.AppVersion, d.LastSeenAt.UTC()).Scan(&got)
	if err != nil {
		return "", fmt.Errorf("upserting device: %w", err)
	}
	return got, nil
}

// TouchDevice updates last_seen_at/status for a known device.
func (s *Store) TouchDevice(ctx context.Context, deviceID string, status domain.DeviceStatus, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE devices SET status = $2, last_seen_at = GREATEST(coalesce(last_seen_at, $3), $3), updated_at = now()
		WHERE id = $1`, deviceID, status, at.UTC())
	return err
}

// GetDevice fetches one device by connector UUID.
func (s *Store) GetDevice(ctx context.Context, id string) (domain.Device, error) {
	return s.scanDevice(s.pool.QueryRow(ctx, deviceCols+` WHERE id = $1`, id))
}

// FindDeviceByExternalID resolves a provider's own device ID.
func (s *Store) FindDeviceByExternalID(ctx context.Context, p domain.ProviderName, externalID string) (domain.Device, error) {
	return s.scanDevice(s.pool.QueryRow(ctx, deviceCols+` WHERE provider = $1 AND external_id = $2`, p, externalID))
}

// ListDevices returns all devices, most recently seen first.
func (s *Store) ListDevices(ctx context.Context) ([]domain.Device, error) {
	rows, err := s.pool.Query(ctx, deviceCols+` ORDER BY last_seen_at DESC NULLS LAST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Device
	for rows.Next() {
		d, err := s.scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

const deviceCols = `SELECT id, provider, external_id, name, status, app_version,
	coalesce(last_seen_at, '0001-01-01'::timestamptz), created_at, updated_at FROM devices`

type rowScanner interface{ Scan(dest ...any) error }

func (s *Store) scanDevice(r rowScanner) (domain.Device, error) {
	var d domain.Device
	err := r.Scan(&d.ID, &d.Provider, &d.ExternalID, &d.Name, &d.Status, &d.AppVersion,
		&d.LastSeenAt, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Device{}, ErrNotFound
	}
	return d, err
}

// ---- Telemetry -------------------------------------------------------------

// InsertTelemetry stores one snapshot.
func (s *Store) InsertTelemetry(ctx context.Context, t domain.TelemetryEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO telemetry (id, device_id, reported_at, received_at, monitoring_status, model_status,
			inference_latency_ms, processed_fps, dropped_frames, confirmed_events, app_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.NewString(), t.DeviceID, t.ReportedAt.UTC(), t.ReceivedAt.UTC(), t.MonitoringStatus,
		t.ModelStatus, t.InferenceLatencyMs, t.ProcessedFPS, t.DroppedFrames, t.ConfirmedEvents, t.AppVersion)
	return err
}

// LatestTelemetry returns the newest snapshot for a device.
func (s *Store) LatestTelemetry(ctx context.Context, deviceID string) (domain.TelemetryEvent, error) {
	var t domain.TelemetryEvent
	err := s.pool.QueryRow(ctx, `
		SELECT id, device_id, reported_at, received_at, monitoring_status, model_status,
			inference_latency_ms, processed_fps, dropped_frames, confirmed_events, app_version
		FROM telemetry WHERE device_id = $1 ORDER BY reported_at DESC LIMIT 1`, deviceID).
		Scan(&t.ID, &t.DeviceID, &t.ReportedAt, &t.ReceivedAt, &t.MonitoringStatus, &t.ModelStatus,
			&t.InferenceLatencyMs, &t.ProcessedFPS, &t.DroppedFrames, &t.ConfirmedEvents, &t.AppVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TelemetryEvent{}, ErrNotFound
	}
	return t, err
}

// ---- Hazard events ---------------------------------------------------------

// InsertHazardEvent persists a normalized event. A duplicate (same source +
// dedup key) returns ErrDuplicate and changes nothing.
func (s *Store) InsertHazardEvent(ctx context.Context, e domain.HazardEvent) (domain.HazardEvent, error) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO hazard_events (id, device_id, source, dedup_key, type, confidence, first_seen_at, confirmed_at, received_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (source, dedup_key) DO NOTHING`,
		e.ID, e.DeviceID, e.Source, e.DedupKey, e.Type, e.Confidence,
		e.FirstSeenAt.UTC(), e.ConfirmedAt.UTC(), e.ReceivedAt.UTC())
	if err != nil {
		return domain.HazardEvent{}, fmt.Errorf("inserting hazard event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.HazardEvent{}, ErrDuplicate
	}
	return e, nil
}

// ListRecentEvents returns up to limit events, newest first.
func (s *Store) ListRecentEvents(ctx context.Context, limit int) ([]domain.HazardEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, source, dedup_key, type, confidence, first_seen_at, confirmed_at, received_at
		FROM hazard_events ORDER BY received_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HazardEvent
	for rows.Next() {
		var e domain.HazardEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Source, &e.DedupKey, &e.Type, &e.Confidence,
			&e.FirstSeenAt, &e.ConfirmedAt, &e.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- Webhook deliveries ----------------------------------------------------

// RecordWebhookDelivery is the idempotency gate for webhooks: the first
// insert wins, a repeat returns ErrDuplicate.
func (s *Store) RecordWebhookDelivery(ctx context.Context, p domain.ProviderName, deliveryID string) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (provider, delivery_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, p, deliveryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicate
	}
	return nil
}

// ---- Dead letters ----------------------------------------------------------

// InsertDeadLetter records a terminally unprocessable payload.
func (s *Store) InsertDeadLetter(ctx context.Context, source domain.ProviderName, reason string, payload []byte) error {
	const maxPayload = 64 << 10
	if len(payload) > maxPayload {
		payload = payload[:maxPayload]
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO dead_letter_events (id, source, reason, payload) VALUES ($1,$2,$3,$4)`,
		uuid.NewString(), source, reason, payload)
	return err
}

// ---- Commands --------------------------------------------------------------

// CreateCommand enqueues a command as PENDING. If an idempotency key is given
// and a command with that key already exists for the device, the existing
// command is returned with ErrDuplicate.
func (s *Store) CreateCommand(ctx context.Context, c domain.Command) (domain.Command, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.State = domain.CommandPending
	payload := c.Payload
	if payload == "" {
		payload = "{}"
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO commands (id, device_id, type, payload, idempotency_key, state, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,'PENDING',now(),$6)
		ON CONFLICT (device_id, idempotency_key) WHERE idempotency_key <> '' DO NOTHING`,
		c.ID, c.DeviceID, c.Type, payload, c.IdempotencyKey, c.ExpiresAt.UTC())
	if err != nil {
		return domain.Command{}, fmt.Errorf("creating command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, err := s.findCommandByIdemKey(ctx, c.DeviceID, c.IdempotencyKey)
		if err != nil {
			return domain.Command{}, err
		}
		return existing, ErrDuplicate
	}
	return s.GetCommand(ctx, c.ID)
}

func (s *Store) findCommandByIdemKey(ctx context.Context, deviceID, key string) (domain.Command, error) {
	return s.scanCommand(s.pool.QueryRow(ctx, commandCols+` WHERE device_id = $1 AND idempotency_key = $2`, deviceID, key))
}

// GetCommand fetches one command.
func (s *Store) GetCommand(ctx context.Context, id string) (domain.Command, error) {
	return s.scanCommand(s.pool.QueryRow(ctx, commandCols+` WHERE id = $1`, id))
}

// FetchPendingCommands atomically claims up to limit PENDING commands for a
// device, marking them DELIVERED. The UPDATE…RETURNING makes concurrent polls
// safe: each command is delivered exactly once.
func (s *Store) FetchPendingCommands(ctx context.Context, deviceID string, limit int) ([]domain.Command, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE commands SET state = 'DELIVERED', delivered_at = now()
		WHERE id IN (
			SELECT id FROM commands
			WHERE device_id = $1 AND state = 'PENDING' AND expires_at > now()
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, device_id, type, payload::text, idempotency_key, state, result_message,
			created_at, delivered_at, completed_at, expires_at`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Command
	for rows.Next() {
		c, err := scanCommandRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TransitionCommand moves a command to a new state, enforcing the state
// machine in SQL: the WHERE clause only matches rows in a legal source state,
// so an illegal transition affects zero rows and is reported as such.
func (s *Store) TransitionCommand(ctx context.Context, id string, to domain.CommandState, resultMessage string) (domain.Command, error) {
	var from []string
	switch to {
	case domain.CommandDelivered:
		from = []string{string(domain.CommandPending)}
	case domain.CommandAcknowledged:
		from = []string{string(domain.CommandDelivered)}
	case domain.CommandFailed:
		from = []string{string(domain.CommandPending), string(domain.CommandDelivered)}
	default:
		return domain.Command{}, ErrInvalidTransition
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE commands SET
			state = $2,
			result_message = CASE WHEN $3 <> '' THEN $3 ELSE result_message END,
			delivered_at = CASE WHEN $2 = 'DELIVERED' THEN now() ELSE delivered_at END,
			completed_at = CASE WHEN $2 IN ('ACKNOWLEDGED','FAILED') THEN now() ELSE completed_at END
		WHERE id = $1 AND state = ANY($4)`,
		id, to, resultMessage, from)
	if err != nil {
		return domain.Command{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetCommand(ctx, id); errors.Is(err, ErrNotFound) {
			return domain.Command{}, ErrNotFound
		}
		return domain.Command{}, ErrInvalidTransition
	}
	return s.GetCommand(ctx, id)
}

// ExpireCommands fails every overdue PENDING/DELIVERED command and returns
// how many were failed. Run periodically by the reaper.
func (s *Store) ExpireCommands(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE commands SET state = 'FAILED', result_message = 'expired before completion', completed_at = now()
		WHERE state IN ('PENDING','DELIVERED') AND expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const commandCols = `SELECT id, device_id, type, payload::text, idempotency_key, state, result_message,
	created_at, delivered_at, completed_at, expires_at FROM commands`

func (s *Store) scanCommand(r rowScanner) (domain.Command, error) {
	c, err := scanCommandRow(r)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Command{}, ErrNotFound
	}
	return c, err
}

func scanCommandRow(r rowScanner) (domain.Command, error) {
	var c domain.Command
	err := r.Scan(&c.ID, &c.DeviceID, &c.Type, &c.Payload, &c.IdempotencyKey, &c.State, &c.ResultMessage,
		&c.CreatedAt, &c.DeliveredAt, &c.CompletedAt, &c.ExpiresAt)
	return c, err
}

// ---- Provider connections --------------------------------------------------

// SaveProviderConnection upserts connection state, encrypting token material
// with the given cipher (which must not be nil when tokens are present).
func (s *Store) SaveProviderConnection(ctx context.Context, pc domain.ProviderConnection, cipher *TokenCipher) error {
	if pc.ID == "" {
		pc.ID = uuid.NewString()
	}
	var access, refresh, secret []byte
	var err error
	if pc.AccessToken != "" || pc.RefreshToken != "" || pc.WebhookSecret != "" {
		if cipher == nil {
			return fmt.Errorf("token cipher required to store credentials")
		}
		if access, err = cipher.Encrypt(pc.AccessToken); err != nil {
			return err
		}
		if refresh, err = cipher.Encrypt(pc.RefreshToken); err != nil {
			return err
		}
		if secret, err = cipher.Encrypt(pc.WebhookSecret); err != nil {
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO provider_connections (id, provider, base_url, access_token_enc, refresh_token_enc, token_expiry, webhook_secret_enc, status, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
		ON CONFLICT (provider) DO UPDATE SET
			base_url = EXCLUDED.base_url,
			access_token_enc = EXCLUDED.access_token_enc,
			refresh_token_enc = EXCLUDED.refresh_token_enc,
			token_expiry = EXCLUDED.token_expiry,
			webhook_secret_enc = EXCLUDED.webhook_secret_enc,
			status = EXCLUDED.status,
			updated_at = now()`,
		pc.ID, pc.Provider, pc.BaseURL, access, refresh, nullableTime(pc.TokenExpiry), secret, orDefault(pc.Status, "ACTIVE"))
	return err
}

// GetProviderConnection loads and decrypts connection state.
func (s *Store) GetProviderConnection(ctx context.Context, p domain.ProviderName, cipher *TokenCipher) (domain.ProviderConnection, error) {
	var pc domain.ProviderConnection
	var access, refresh, secret []byte
	var expiry *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, base_url, access_token_enc, refresh_token_enc, token_expiry, webhook_secret_enc, status, updated_at
		FROM provider_connections WHERE provider = $1`, p).
		Scan(&pc.ID, &pc.Provider, &pc.BaseURL, &access, &refresh, &expiry, &secret, &pc.Status, &pc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProviderConnection{}, ErrNotFound
	}
	if err != nil {
		return domain.ProviderConnection{}, err
	}
	if expiry != nil {
		pc.TokenExpiry = *expiry
	}
	if cipher != nil {
		if pc.AccessToken, err = cipher.Decrypt(access); err != nil {
			return domain.ProviderConnection{}, err
		}
		if pc.RefreshToken, err = cipher.Decrypt(refresh); err != nil {
			return domain.ProviderConnection{}, err
		}
		if pc.WebhookSecret, err = cipher.Decrypt(secret); err != nil {
			return domain.ProviderConnection{}, err
		}
	}
	return pc, nil
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
