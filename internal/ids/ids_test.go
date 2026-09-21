package ids_test

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/ids"
)

func TestULIDsAreWellShapedAndUnique(t *testing.T) {
	source := ids.NewULIDSource()
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := source.New("evt")
		if !ids.Valid(id) {
			t.Fatalf("generated identifier %q is not well shaped", id)
		}
		if seen[id] {
			t.Fatalf("identifier %q was generated twice", id)
		}
		seen[id] = true
	}
}

// TestULIDsSortByCreationTime is the property that lets durable records be
// listed in creation order without a secondary sort key.
func TestULIDsSortByCreationTime(t *testing.T) {
	source := ids.NewULIDSource()
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var generated []string
	for i := 0; i < 50; i++ {
		generated = append(generated, source.NewAt("evt", base.Add(time.Duration(i)*time.Second)))
	}
	sorted := append([]string(nil), generated...)
	sort.Strings(sorted)
	for i := range generated {
		if generated[i] != sorted[i] {
			t.Fatalf("identifiers do not sort by creation time at position %d", i)
		}
	}
}

func TestSequentialSourceIsDeterministic(t *testing.T) {
	first := ids.NewSequential()
	second := ids.NewSequential()
	for i := 0; i < 10; i++ {
		a, b := first.New("evt"), second.New("evt")
		if a != b {
			t.Fatalf("two fresh sequential sources diverged: %q vs %q", a, b)
		}
		if !ids.Valid(a) {
			t.Fatalf("deterministic identifier %q is not well shaped", a)
		}
	}
	// Counters are per prefix, so a task identifier does not depend on how
	// many events happened to be created alongside it.
	if first.New("tsk") != second.New("tsk") {
		t.Fatal("per-prefix counters diverged")
	}
}

func TestSequentialNewAtIgnoresTime(t *testing.T) {
	source := ids.NewSequential()
	early := source.NewAt("evt", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	source.Reset()
	late := source.NewAt("evt", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if early != late {
		t.Fatalf("deterministic identifiers depend on time: %q vs %q", early, late)
	}
}

func TestSourcesAreSafeForConcurrentUse(t *testing.T) {
	for name, source := range map[string]ids.Source{
		"ulid":       ids.NewULIDSource(),
		"sequential": ids.NewSequential(),
	} {
		t.Run(name, func(t *testing.T) {
			var wg sync.WaitGroup
			var mu sync.Mutex
			seen := make(map[string]bool)
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 64; j++ {
						id := source.New("evt")
						mu.Lock()
						if seen[id] {
							t.Errorf("identifier %q was generated twice", id)
						}
						seen[id] = true
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
		})
	}
}

func TestValidRejectsMalformedIdentifiers(t *testing.T) {
	for _, id := range []string{
		"", "evt_", "_0000000000000000000000000A", "evt_short",
		"evt_0000000000000000000000000I", // I is excluded from Crockford base32
		"evt_0000000000000000000000000U",
	} {
		if ids.Valid(id) {
			t.Errorf("%q was accepted as a valid identifier", id)
		}
	}
}

// TestValidChecksThePrefixToo keeps the shape check honest about the shape it
// documents. A prefix of arbitrary bytes would let identifiers this package
// never produced pass a check whose only job is to say whether they look like
// it did.
func TestValidChecksThePrefixToo(t *testing.T) {
	generated := ids.NewULIDSource().New("tsk")
	body := generated[len("tsk_"):]
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"tsk_" + body, true},
		{body, true},
		{"TSK_" + body, false},
		{"evt-1_" + body, false},
		{"tsk9_" + body, false},
		{"_" + body, false},
	} {
		if got := ids.Valid(tc.id); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
