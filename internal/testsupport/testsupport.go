// Package testsupport provides the deterministic fixtures the control-plane
// test suite is built on.
//
// ENGINEERING_STANDARDS.md §19 requires the hard control-plane logic to be
// testable offline: nothing here starts a model runtime, touches a
// repository, or reads the wall clock. A scenario built through this package
// produces byte-identical events on every run, which is what lets tests
// assert on whole documents instead of on selected fields.
package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/observability"
	"github.com/olostan/DevCadience/internal/storage"
)

// Epoch is the instant every deterministic clock starts at. It is a fixed
// point in the past so that fixtures never depend on "now".
var Epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// Step is how far the deterministic clock advances per reading. One second is
// large enough to be readable in golden output and small enough that a long
// scenario stays within one day.
const Step = time.Second

// NewClock returns a deterministic clock starting at Epoch.
func NewClock() *clock.Fake { return clock.NewFake(Epoch, Step) }

// NewIDs returns a deterministic identifier source.
func NewIDs() *ids.Sequential { return ids.NewSequential() }

// Harness bundles a store and a service wired to deterministic sources.
type Harness struct {
	Store   *storage.Store
	Service *controlplane.Service
	Clock   *clock.Fake
	IDs     *ids.Sequential
}

// NewHarness opens an in-memory control plane for a test.
//
// The database is in-memory so that a test needs no filesystem and leaves
// nothing behind; NewFileHarness covers the cases that must survive a reopen.
func NewHarness(t *testing.T) *Harness {
	t.Helper()
	return newHarness(t, storage.MemoryPath)
}

// NewFileHarness opens a control plane backed by a file in the test's
// temporary directory, for tests that reopen a persisted database.
func NewFileHarness(t *testing.T, path string) *Harness {
	t.Helper()
	return newHarness(t, path)
}

func newHarness(t *testing.T, path string) *Harness {
	t.Helper()
	testClock := NewClock()
	testIDs := NewIDs()
	store, err := storage.Open(context.Background(), storage.Config{Path: path, Clock: testClock})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	service, err := controlplane.New(controlplane.Options{
		Store:  store,
		Clock:  testClock,
		IDs:    testIDs,
		Logger: observability.NewLogger(observability.Options{}),
	})
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return &Harness{Store: store, Service: service, Clock: testClock, IDs: testIDs}
}
