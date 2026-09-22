package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
)

func TestFakeClockAdvancesDeterministically(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fake := clock.NewFake(start, time.Second)
	for i := 0; i < 5; i++ {
		want := start.Add(time.Duration(i) * time.Second)
		if got := fake.Now(); !got.Equal(want) {
			t.Fatalf("reading %d = %s, want %s", i, got, want)
		}
	}
}

func TestFrozenFakeClockDoesNotMove(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fake := clock.NewFake(start, 0)
	first := fake.Now()
	second := fake.Now()
	if !first.Equal(second) {
		t.Fatalf("a zero-step clock moved: %s then %s", first, second)
	}
	fake.Advance(time.Hour)
	if got := fake.Now(); !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("Advance did not move the clock: %s", got)
	}
}

func TestSystemClockIsUTCAndMicrosecondTruncated(t *testing.T) {
	now := clock.System().Now()
	if now.Location() != time.UTC {
		t.Fatalf("system clock returned %s, want UTC", now.Location())
	}
	// Sub-microsecond precision would break round-tripping through RFC3339
	// text and SQLite, which the rebuild guarantee depends on.
	if now.Nanosecond()%1000 != 0 {
		t.Fatalf("system clock returned sub-microsecond precision: %s", now)
	}
}

func TestFakeClockIsSafeForConcurrentUse(t *testing.T) {
	fake := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Millisecond)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[time.Time]bool)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				now := fake.Now()
				mu.Lock()
				if seen[now] {
					t.Errorf("instant %s was handed out twice", now)
				}
				seen[now] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
