package limiter

import (
	"context"
	"testing"
	"time"
)

func TestZeroRateDoesNotWait(t *testing.T) {
	l := New(0)
	start := time.Now()
	if err := l.WaitN(context.Background(), 10<<20); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("a zero limit waited")
	}
}

func TestRateHoldsBack(t *testing.T) {
	l := New(64 << 10) // 64 KiB/s
	start := time.Now()
	// 32 KiB should take about half a second.
	if err := l.WaitN(context.Background(), 32<<10); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 200*time.Millisecond {
		t.Fatalf("32 KiB at 64 KiB/s took %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("too slow: %v", elapsed)
	}
}

func TestCancelStopsTheWait(t *testing.T) {
	l := New(1024)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := l.WaitN(ctx, 1<<20); err == nil {
		t.Fatal("a canceled wait returned nil")
	}
}
