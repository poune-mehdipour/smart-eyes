// Package reliability centralizes the service's retry policy. Every outbound
// call that may fail transiently goes through Do, so backoff behavior,
// error classification and context handling live in exactly one place.
package reliability

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Permanent wraps an error to tell Do that retrying cannot help (a 4xx from a
// provider, a validation failure, an invalid refresh token). Do returns the
// wrapped error immediately.
type Permanent struct{ Err error }

func (p Permanent) Error() string { return p.Err.Error() }
func (p Permanent) Unwrap() error { return p.Err }

// IsPermanent reports whether err is (or wraps) a Permanent error.
func IsPermanent(err error) bool {
	var p Permanent
	return errors.As(err, &p)
}

// Policy describes an exponential backoff with full jitter.
//
// Delay before attempt n (0-based) is drawn uniformly from
// [0, min(Base*2^n, Max)] — "full jitter", which avoids retry stampedes when
// many callers fail at once. Attempts is the total number of tries.
type Policy struct {
	Attempts int
	Base     time.Duration
	Max      time.Duration
}

// DefaultPolicy suits calls to providers and the database: 5 tries over
// roughly 15 seconds worst case.
func DefaultPolicy() Policy {
	return Policy{Attempts: 5, Base: 200 * time.Millisecond, Max: 5 * time.Second}
}

// Do runs op until it succeeds, returns a permanent error, exhausts the
// policy, or the context is done. Context cancellation always wins: an
// in-flight sleep is interrupted and ctx.Err() is returned.
func Do(ctx context.Context, p Policy, op func(ctx context.Context) error) error {
	if p.Attempts < 1 {
		p.Attempts = 1
	}
	var last error
	for attempt := 0; attempt < p.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = op(ctx)
		if last == nil {
			return nil
		}
		if IsPermanent(last) || errors.Is(last, context.Canceled) || errors.Is(last, context.DeadlineExceeded) {
			return last
		}
		if attempt == p.Attempts-1 {
			break
		}
		select {
		case <-time.After(backoff(p, attempt)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("giving up after %d attempts: %w", p.Attempts, last)
}

func backoff(p Policy, attempt int) time.Duration {
	ceil := p.Base << uint(attempt) // Base * 2^attempt
	if ceil > p.Max || ceil <= 0 {
		ceil = p.Max
	}
	return rand.N(ceil + 1)
}
