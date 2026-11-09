// Package limiter is a token bucket shared by the connections of one
// download, or by every download, so a speed limit is a real ceiling.
package limiter

import (
	"context"
	"sync"
	"time"
)

// Limiter hands out bytes at a rate in bytes per second. A zero or
// negative rate lets everything through.
type Limiter struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	last   time.Time
}

// New makes a limiter at rate bytes per second.
func New(rate int64) *Limiter {
	l := &Limiter{last: time.Now()}
	l.SetRate(rate)
	return l
}

// SetRate changes the ceiling. Running waits see the new rate.
func (l *Limiter) SetRate(rate int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rate < 0 {
		rate = 0
	}
	l.rate = float64(rate)
	if l.rate == 0 {
		l.tokens = 0
		return
	}
	// A burst of a quarter second keeps small reads smooth without
	// letting a fast connection jump far ahead.
	burst := l.rate / 4
	if burst < 16<<10 {
		burst = 16 << 10
	}
	if l.tokens > burst {
		l.tokens = burst
	}
}

// Rate is the current ceiling in bytes per second, 0 for none.
func (l *Limiter) Rate() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int64(l.rate)
}

// WaitN blocks until n bytes may be sent, or ctx is done. A read larger
// than the bucket is paid for in bucket-sized pieces, so a big buffer
// never waits for a token count the bucket cannot hold.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	for n > 0 {
		l.mu.Lock()
		rate := l.rate
		if rate == 0 {
			l.mu.Unlock()
			return nil
		}
		burst := rate / 4
		if burst < 16<<10 {
			burst = 16 << 10
		}
		take := float64(n)
		if take > burst {
			take = burst
		}

		now := time.Now()
		if !l.last.IsZero() {
			l.tokens += now.Sub(l.last).Seconds() * rate
		}
		l.last = now
		if l.tokens > burst {
			l.tokens = burst
		}
		if l.tokens >= take {
			l.tokens -= take
			l.mu.Unlock()
			n -= int(take)
			continue
		}
		need := take - l.tokens
		wait := time.Duration(need / rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// Group applies a per-download limit and a global limit together.
type Group struct {
	Download *Limiter
	Global   *Limiter
}

// WaitN waits on both limiters.
func (g Group) WaitN(ctx context.Context, n int) error {
	if g.Download != nil {
		if err := g.Download.WaitN(ctx, n); err != nil {
			return err
		}
	}
	if g.Global != nil {
		if err := g.Global.WaitN(ctx, n); err != nil {
			return err
		}
	}
	return nil
}
