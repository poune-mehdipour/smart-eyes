package reliability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fastPolicy(attempts int) Policy {
	return Policy{Attempts: attempts, Base: time.Millisecond, Max: 2 * time.Millisecond}
}

func TestDo(t *testing.T) {
	sentinel := errors.New("boom")

	tests := []struct {
		name      string
		policy    Policy
		failures  int  // op fails this many times, then succeeds
		permanent bool // failures are permanent
		wantCalls int
		wantErr   bool
		wantSame  bool // returned error wraps sentinel
	}{
		{name: "first try succeeds", policy: fastPolicy(5), failures: 0, wantCalls: 1},
		{name: "transient failures then success", policy: fastPolicy(5), failures: 3, wantCalls: 4},
		{name: "exhausts attempts", policy: fastPolicy(3), failures: 99, wantCalls: 3, wantErr: true, wantSame: true},
		{name: "permanent error stops immediately", policy: fastPolicy(5), failures: 99, permanent: true, wantCalls: 1, wantErr: true, wantSame: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := Do(context.Background(), tt.policy, func(context.Context) error {
				calls++
				if calls <= tt.failures {
					if tt.permanent {
						return Permanent{Err: sentinel}
					}
					return sentinel
				}
				return nil
			})
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantSame && !errors.Is(err, sentinel) {
				t.Fatalf("err %v does not wrap sentinel", err)
			}
		})
	}
}

func TestDoContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	// Long backoff: cancellation must interrupt the sleep, not wait it out.
	policy := Policy{Attempts: 5, Base: 10 * time.Second, Max: 10 * time.Second}
	done := make(chan error, 1)
	go func() {
		done <- Do(ctx, policy, func(context.Context) error {
			calls++
			return errors.New("transient")
		})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1 (no retry after cancel)", calls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do did not return promptly after cancellation")
	}
}

func TestDoAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := Do(ctx, fastPolicy(3), func(context.Context) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("err=%v calls=%d; want Canceled and 0 calls", err, calls)
	}
}

func TestBackoffBounds(t *testing.T) {
	p := Policy{Attempts: 10, Base: 100 * time.Millisecond, Max: time.Second}
	for attempt := 0; attempt < 10; attempt++ {
		for i := 0; i < 50; i++ {
			d := backoff(p, attempt)
			if d < 0 || d > p.Max {
				t.Fatalf("backoff(attempt %d) = %v outside [0, %v]", attempt, d, p.Max)
			}
		}
	}
}
