package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
)

// fakeStore mimics the Postgres command semantics in memory, including the
// transition guard and the idempotency-key constraint.
type fakeStore struct {
	devices  map[string]domain.Device
	commands map[string]*domain.Command
	byIdem   map[string]string // deviceID/key -> commandID
	seq      int
}

func newFakeStore() *fakeStore {
	return &fakeStore{devices: map[string]domain.Device{}, commands: map[string]*domain.Command{}, byIdem: map[string]string{}}
}

func (f *fakeStore) addDevice(p domain.ProviderName) string {
	f.seq++
	id := fmt.Sprintf("dev-%d", f.seq)
	f.devices[id] = domain.Device{ID: id, ExternalID: "ext-" + id, Provider: p}
	return id
}

func (f *fakeStore) GetDevice(_ context.Context, id string) (domain.Device, error) {
	d, ok := f.devices[id]
	if !ok {
		return domain.Device{}, storage.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) FindDeviceByExternalID(_ context.Context, p domain.ProviderName, ext string) (domain.Device, error) {
	for _, d := range f.devices {
		if d.Provider == p && d.ExternalID == ext {
			return d, nil
		}
	}
	return domain.Device{}, storage.ErrNotFound
}

func (f *fakeStore) CreateCommand(_ context.Context, c domain.Command) (domain.Command, error) {
	if c.IdempotencyKey != "" {
		if existing, ok := f.byIdem[c.DeviceID+"/"+c.IdempotencyKey]; ok {
			return *f.commands[existing], storage.ErrDuplicate
		}
	}
	f.seq++
	c.ID = fmt.Sprintf("cmd-%d", f.seq)
	c.State = domain.CommandPending
	c.CreatedAt = time.Now()
	f.commands[c.ID] = &c
	if c.IdempotencyKey != "" {
		f.byIdem[c.DeviceID+"/"+c.IdempotencyKey] = c.ID
	}
	return c, nil
}

func (f *fakeStore) GetCommand(_ context.Context, id string) (domain.Command, error) {
	c, ok := f.commands[id]
	if !ok {
		return domain.Command{}, storage.ErrNotFound
	}
	return *c, nil
}

func (f *fakeStore) FetchPendingCommands(_ context.Context, deviceID string, limit int) ([]domain.Command, error) {
	var out []domain.Command
	for _, c := range f.commands {
		if c.DeviceID == deviceID && c.State == domain.CommandPending && len(out) < limit {
			c.State = domain.CommandDelivered
			now := time.Now()
			c.DeliveredAt = &now
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakeStore) TransitionCommand(_ context.Context, id string, to domain.CommandState, msg string) (domain.Command, error) {
	c, ok := f.commands[id]
	if !ok {
		return domain.Command{}, storage.ErrNotFound
	}
	if !domain.CanTransition(c.State, to) {
		return domain.Command{}, storage.ErrInvalidTransition
	}
	c.State = to
	if msg != "" {
		c.ResultMessage = msg
	}
	return *c, nil
}

func (f *fakeStore) ExpireCommands(_ context.Context) (int64, error) {
	var n int64
	for _, c := range f.commands {
		if (c.State == domain.CommandPending || c.State == domain.CommandDelivered) && time.Now().After(c.ExpiresAt) {
			c.State = domain.CommandFailed
			n++
		}
	}
	return n, nil
}

// fakeSender is a scriptable provider.CommandSender.
type fakeSender struct {
	name  domain.ProviderName
	state domain.CommandState
	err   error
	sent  []domain.Command
}

func (s *fakeSender) Name() domain.ProviderName { return s.name }
func (s *fakeSender) SendCommand(_ context.Context, _ string, c domain.Command) (domain.CommandState, string, error) {
	s.sent = append(s.sent, c)
	return s.state, "ok", s.err
}

func newService(store *fakeStore, senders map[domain.ProviderName]provider.CommandSender) *Service {
	return New(store, slog.New(slog.DiscardHandler), observability.NewMetrics(), time.Hour, senders)
}

// ---- Tests -----------------------------------------------------------------

func TestEdgeCommandLifecycleHappyPath(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderGuardianEdge)
	svc := newService(store, nil)
	ctx := context.Background()

	cmd, err := svc.Create(ctx, dev, domain.CommandGetStatus, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.State != domain.CommandPending {
		t.Fatalf("state = %v, want PENDING", cmd.State)
	}

	polled, err := svc.Poll(ctx, dev, 10)
	if err != nil || len(polled) != 1 {
		t.Fatalf("polled = %v, err = %v", polled, err)
	}
	if polled[0].State != domain.CommandDelivered {
		t.Fatalf("state after poll = %v, want DELIVERED", polled[0].State)
	}

	done, err := svc.ReportResult(ctx, cmd.ID, true, "monitoring is on")
	if err != nil {
		t.Fatal(err)
	}
	if done.State != domain.CommandAcknowledged || done.ResultMessage != "monitoring is on" {
		t.Fatalf("final = %+v", done)
	}
}

func TestCreateValidation(t *testing.T) {
	store := newFakeStore()
	edge := store.addDevice(domain.ProviderGuardianEdge)
	svc := newService(store, nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		device  string
		typ     domain.CommandType
		payload string
	}{
		{"unknown command type", edge, domain.CommandType("SELF_DESTRUCT"), ""},
		{"invalid payload JSON", edge, domain.CommandUpdateConfig, "{not json"},
		{"unknown device", "no-such-device", domain.CommandGetStatus, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(ctx, tt.device, tt.typ, tt.payload, "")
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestIdempotencyKeyReturnsExistingCommand(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderGuardianEdge)
	svc := newService(store, nil)
	ctx := context.Background()

	first, err := svc.Create(ctx, dev, domain.CommandStartMonitoring, "", "op-retry-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, dev, domain.CommandStartMonitoring, "", "op-retry-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate create made a new command: %s vs %s", first.ID, second.ID)
	}
	if len(store.commands) != 1 {
		t.Fatalf("stored commands = %d, want 1", len(store.commands))
	}
}

func TestProviderPushDispatchSuccess(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderAlphaSense)
	sender := &fakeSender{name: domain.ProviderAlphaSense, state: domain.CommandDelivered}
	svc := newService(store, map[domain.ProviderName]provider.CommandSender{domain.ProviderAlphaSense: sender})

	cmd, err := svc.Create(context.Background(), dev, domain.CommandStopMonitoring, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.State != domain.CommandDelivered {
		t.Fatalf("state = %v, want DELIVERED after push", cmd.State)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sender.sent = %d", len(sender.sent))
	}
}

func TestProviderPushDispatchFailureRecordsFAILED(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderAlphaSense)
	sender := &fakeSender{name: domain.ProviderAlphaSense, err: errors.New("provider melted")}
	svc := newService(store, map[domain.ProviderName]provider.CommandSender{domain.ProviderAlphaSense: sender})

	cmd, err := svc.Create(context.Background(), dev, domain.CommandGetStatus, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.State != domain.CommandFailed {
		t.Fatalf("state = %v, want FAILED", cmd.State)
	}
	if cmd.ResultMessage == "" {
		t.Fatal("failure reason must be recorded")
	}
}

func TestCommandToProviderWithoutCapabilityIsRejected(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderBetaGrid) // no CommandSender registered
	svc := newService(store, nil)

	_, err := svc.Create(context.Background(), dev, domain.CommandGetStatus, "", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (betagrid takes no commands)", err)
	}
	if len(store.commands) != 0 {
		t.Fatal("no command may be stored for an incapable provider")
	}
}

func TestResultOnUndeliveredCommandIsConflict(t *testing.T) {
	store := newFakeStore()
	dev := store.addDevice(domain.ProviderGuardianEdge)
	svc := newService(store, nil)
	ctx := context.Background()

	cmd, _ := svc.Create(ctx, dev, domain.CommandGetStatus, "", "")
	// Device reports a result without having polled: PENDING → ACKNOWLEDGED
	// is not a legal transition.
	if _, err := svc.ReportResult(ctx, cmd.ID, true, ""); !errors.Is(err, storage.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestStateMachineTable(t *testing.T) {
	legal := map[[2]domain.CommandState]bool{
		{domain.CommandPending, domain.CommandDelivered}:      true,
		{domain.CommandPending, domain.CommandFailed}:         true,
		{domain.CommandDelivered, domain.CommandAcknowledged}: true,
		{domain.CommandDelivered, domain.CommandFailed}:       true,
	}
	states := []domain.CommandState{domain.CommandPending, domain.CommandDelivered, domain.CommandAcknowledged, domain.CommandFailed}
	for _, from := range states {
		for _, to := range states {
			want := legal[[2]domain.CommandState{from, to}]
			if got := domain.CanTransition(from, to); got != want {
				t.Fatalf("CanTransition(%s → %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}
