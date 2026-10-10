package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

var (
	testServerBinaryPath string
	testServerBuildOnce  sync.Once
	testServerBuildErr   error
)

func getTestServerBinary(t *testing.T) string {
	t.Helper()
	testServerBuildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "devcadence_test_server_*")
		if err != nil {
			testServerBuildErr = err
			return
		}
		srcPath := filepath.Join(dir, "server.go")
		binPath := filepath.Join(dir, "server")

		src := `package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--port=") {
			port = strings.TrimPrefix(arg, "--port=")
		}
	}
	if port == "" {
		port = "8080"
	}

	if os.Getenv("PREMATURE_EXIT") == "1" {
		time.Sleep(100 * time.Millisecond)
		os.Exit(42)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "healthy")
	})

	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
}
`
		if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
			testServerBuildErr = err
			return
		}

		cmd := exec.Command("go", "build", "-o", binPath, srcPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			testServerBuildErr = fmt.Errorf("build test server: %w: %s", err, string(out))
			return
		}
		testServerBinaryPath = binPath
	})

	if testServerBuildErr != nil {
		t.Fatalf("Failed to build test server binary: %v", testServerBuildErr)
	}
	return testServerBinaryPath
}

func setupTestArtifacts(t *testing.T) *artifacts.Store {
	t.Helper()
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestConcurrentValidationServices(t *testing.T) {
	serverBin := getTestServerBinary(t)
	store := setupTestArtifacts(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	ports := make([]int, 2)

	for i := 0; i < 2; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			runID := fmt.Sprintf("run_%d_%d", idx, time.Now().UnixNano())
			dir := t.TempDir()

			profile := Profile{
				Name: "test-concurrent",
				Services: []ServiceSpec{
					{
						ID:              fmt.Sprintf("srv-%d", idx),
						Argv:            []string{serverBin},
						StartupTimeout:  5 * time.Second,
						ShutdownTimeout: 2 * time.Second,
						ReadinessProbe: ReadinessProbe{
							Kind:           "http_get",
							Path:           "/health",
							ExpectedStatus: 200,
							ExpectedBody:   "healthy",
						},
						PortConfig: PortConfig{
							Mode:       PortHandoffEnvVar,
							EnvVarName: "PORT",
						},
					},
				},
				Checks: []CheckSpec{
					{
						ID:      "check-health",
						Argv:    []string{"sh", "-c", "test -n \"$PORT\""},
						Timeout: 2 * time.Second,
					},
				},
			}

			active, err := StartServices(context.Background(), profile.Services, dir, nil, runID)
			if err != nil {
				errCh <- fmt.Errorf("run %d StartServices: %w", idx, err)
				return
			}
			defer active.Teardown(context.Background())

			ports[idx] = active.services[0].Port

			checks, outcome, err := RunProfile(context.Background(), profile, RunOptions{
				Dir:       dir,
				Artifacts: store,
				ProjectID: "proj_concurrent",
				RunID:     runID,
			})
			if err != nil {
				errCh <- fmt.Errorf("run %d RunProfile: %w", idx, err)
				return
			}
			if outcome != protocol.ValidationPass {
				errCh <- fmt.Errorf("run %d failed with outcome %v: %+v", idx, outcome, checks)
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	if ports[0] == ports[1] {
		t.Errorf("Expected distinct ports for concurrent services, but both got %d", ports[0])
	}
}

func TestServicePrematureExit(t *testing.T) {
	serverBin := getTestServerBinary(t)
	dir := t.TempDir()
	store := setupTestArtifacts(t)

	profile := Profile{
		Name: "test-premature-exit",
		Services: []ServiceSpec{
			{
				ID:              "srv-dying",
				Argv:            []string{serverBin},
				StartupTimeout:  5 * time.Second,
				ShutdownTimeout: 2 * time.Second,
				Env: map[string]string{
					"PREMATURE_EXIT": "1",
				},
				ReadinessProbe: ReadinessProbe{
					Kind: "tcp_port",
				},
				PortConfig: PortConfig{
					Mode:       PortHandoffEnvVar,
					EnvVarName: "PORT",
				},
			},
		},
		Checks: []CheckSpec{
			{
				ID:      "check-1",
				Argv:    []string{"sh", "-c", "sleep 0.3"},
				Timeout: 2 * time.Second,
			},
			{
				ID:      "check-2",
				Argv:    []string{"echo", "should-not-reach-here"},
				Timeout: 2 * time.Second,
			},
		},
	}

	checks, outcome, err := RunProfile(context.Background(), profile, RunOptions{
		Dir:       dir,
		Artifacts: store,
		ProjectID: "proj_dying",
	})
	// Should fail during RunProfile check loop with ValidationError
	if outcome != protocol.ValidationError {
		t.Fatalf("Expected validation error due to premature exit, got outcome=%s, err=%v", outcome, err)
	}
	if len(checks) > 1 {
		t.Fatalf("Expected fail-fast after service death (check-2 skipped), got %d checks", len(checks))
	}
}

func TestServiceVerifiedTeardown(t *testing.T) {
	serverBin := getTestServerBinary(t)
	dir := t.TempDir()

	profile := Profile{
		Name: "test-teardown",
		Services: []ServiceSpec{
			{
				ID:             "srv-teardown",
				Argv:           []string{serverBin},
				StartupTimeout: 5 * time.Second,
				ReadinessProbe: ReadinessProbe{
					Kind:           "http_get",
					Path:           "/health",
					ExpectedStatus: 200,
				},
				PortConfig: PortConfig{
					Mode:       PortHandoffEnvVar,
					EnvVarName: "PORT",
				},
			},
		},
	}

	active, err := StartServices(context.Background(), profile.Services, dir, nil, "run_teardown")
	if err != nil {
		t.Fatalf("StartServices: %v", err)
	}
	t.Cleanup(func() { _ = active.Teardown(context.Background()) })

	pid := active.services[0].PID
	tempDir := active.services[0].TempDir
	pidFile := filepath.Join(tempDir, "service.pid")

	// Verify PID is running
	if !isPIDAlive(pid) {
		t.Fatalf("Service process %d is not running", pid)
	}

	// Read PID record
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("ReadFile pid file: %v", err)
	}
	var record PIDRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("Unmarshal PID record: %v", err)
	}
	osStartTime, err := processStartTime(context.Background(), pid)
	if err != nil {
		t.Fatalf("Read OS process start time: %v", err)
	}
	if diff := record.StartTime.Sub(osStartTime); diff < -processStartTimeTolerance || diff > processStartTimeTolerance {
		t.Fatalf("PID record start time %v differs from OS identity %v", record.StartTime, osStartTime)
	}

	// Verify ownership check succeeds on actual running process
	if !VerifyProcessOwnership(record) {
		t.Errorf("Expected VerifyProcessOwnership to return true for active process")
	}

	// Tamper with start time in record: simulates PID recycling
	staleRecord := record
	staleRecord.StartTime = record.StartTime.Add(-24 * time.Hour)
	if VerifyProcessOwnership(staleRecord) {
		t.Errorf("Expected VerifyProcessOwnership to return false for recycled PID with mismatched start time")
	}

	// Test ReconcileAndCleanup refuses to kill process when start time doesn't match
	stalePIDFile := filepath.Join(t.TempDir(), "stale.pid")
	staleData, _ := json.Marshal(staleRecord)
	_ = os.WriteFile(stalePIDFile, staleData, 0o600)

	err = ReconcileAndCleanup(stalePIDFile)
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("Expected CategoryPolicyDenied for stale PID reconciliation, got: %v", err)
	}
	if !isPIDAlive(pid) {
		t.Fatal("Stale PID reconciliation signaled the live service")
	}

	// Teardown active service
	if err := active.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	// Give OS brief moment to reap
	time.Sleep(100 * time.Millisecond)

	// Verify process is killed
	if isPIDAlive(pid) {
		t.Errorf("Expected process %d to be dead after Teardown, but it is still running", pid)
	}
}

func TestServicePersistsOSStartTime(t *testing.T) {
	serverBin := getTestServerBinary(t)
	// An OS timestamp deliberately far from the controller wall clock proves
	// persistence does not substitute time.Now() for process identity.
	want := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	spec := ServiceSpec{ID: "clock-skew", Argv: []string{serverBin}, Env: map[string]string{"PORT": "0"}}
	svc, err := startSingleServiceWithIdentity(context.Background(), spec, t.TempDir(), nil, "clock-skew", nil,
		func(context.Context, int) (time.Time, error) { return want, nil })
	if err != nil {
		t.Fatal(err)
	}
	active := &ActiveServices{services: []*ActiveService{svc}}
	t.Cleanup(func() { _ = active.Teardown(context.Background()) })
	data, err := os.ReadFile(filepath.Join(svc.TempDir, "service.pid"))
	if err != nil {
		t.Fatal(err)
	}
	var record PIDRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.PID != svc.PID || !record.StartTime.Equal(want) {
		t.Fatalf("persisted identity = %+v; want PID %d, start %v", record, svc.PID, want)
	}
}

func TestServiceIdentityFailureReapsChild(t *testing.T) {
	serverBin := getTestServerBinary(t)
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	var pid int
	spec := ServiceSpec{ID: "identity-failure", Argv: []string{serverBin}, Env: map[string]string{"PORT": "0"}}
	svc, err := startSingleServiceWithIdentity(context.Background(), spec, t.TempDir(), nil, "identity-failure", nil,
		func(_ context.Context, childPID int) (time.Time, error) {
			pid = childPID
			if !isPIDAlive(pid) {
				t.Error("identity lookup did not receive a live child")
			}
			return time.Time{}, os.ErrPermission
		})
	if svc != nil || err == nil || errs.CategoryOf(err) != errs.CategoryInternal {
		t.Fatalf("identity failure: service=%v error=%v", svc, err)
	}
	if pid <= 0 || isPIDAlive(pid) {
		t.Fatalf("child %d was not terminated and reaped", pid)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("startup left temporary resources: entries=%v error=%v", entries, err)
	}
}

func TestProcessStartTimeParsing(t *testing.T) {
	for _, input := range []string{"", "   \n", "not a timestamp", "Mon Jan 99 15:04:05 2006"} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseProcessStartTime(123, input); err == nil {
				t.Fatal("accepted missing or malformed process identity")
			}
		})
	}
	want := time.Date(2006, time.January, 2, 15, 4, 5, 0, time.Local)
	got, err := parseProcessStartTime(123, "  Mon Jan  2 15:04:05 2006\n")
	if err != nil || !got.Equal(want) {
		t.Fatalf("parsed identity=%v error=%v; want %v", got, err, want)
	}
}

func TestProcessStartTimeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queryProcessStartTime(ctx, os.Getpid()); err == nil {
		t.Fatal("cancelled identity query succeeded")
	}
}

func TestProcessNamespaceIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      string
		err       error
		wantError bool
	}{
		{name: "same namespace", data: "42 (service name) S", wantError: false},
		{name: "different namespace", data: "142 (service) S", wantError: true},
		{name: "missing", err: os.ErrNotExist, wantError: true},
		{name: "empty", wantError: true},
		{name: "malformed PID", data: "invalid (service) S", wantError: true},
		{name: "missing stat fields", data: "42", wantError: true},
		{name: "zero PID", data: "0 (service) S", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkLinuxProcessNamespace(42, func() ([]byte, error) { return []byte(tc.data), tc.err })
			if (err != nil) != tc.wantError {
				t.Fatalf("namespace check error=%v; want error=%v", err, tc.wantError)
			}
			queryCalled := false
			got, queryErr := processStartTimeWithNamespace(context.Background(), 42,
				func() error { return err },
				func(context.Context, int) (time.Time, error) {
					queryCalled = true
					// A colliding outer PID can return a valid timestamp. A failed
					// namespace check must reject it without querying that PID.
					return parseProcessStartTime(42, "Mon Jan  2 15:04:05 2006")
				})
			if tc.wantError {
				if queryCalled || !got.IsZero() || !errors.Is(queryErr, err) {
					t.Fatalf("unsafe namespace query: called=%v time=%v error=%v", queryCalled, got, queryErr)
				}
			} else if !queryCalled || queryErr != nil || got.IsZero() {
				t.Fatalf("valid namespace rejected: called=%v time=%v error=%v", queryCalled, got, queryErr)
			}
		})
	}
}

func TestReconciliationRejectsMismatchedProcessNamespace(t *testing.T) {
	serverBin := getTestServerBinary(t)
	want := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	spec := ServiceSpec{ID: "namespace-refusal", Argv: []string{serverBin}, Env: map[string]string{"PORT": "0"}}
	svc, err := startSingleServiceWithIdentity(context.Background(), spec, t.TempDir(), nil, "namespace-refusal", nil,
		func(context.Context, int) (time.Time, error) { return want, nil })
	if err != nil {
		t.Fatal(err)
	}
	active := &ActiveServices{services: []*ActiveService{svc}}
	t.Cleanup(func() { _ = active.Teardown(context.Background()) })
	pidFile := filepath.Join(svc.TempDir, "service.pid")
	queryCalled := false
	err = reconcileAndCleanupWithIdentity(pidFile, func(ctx context.Context, pid int) (time.Time, error) {
		if pid != svc.PID {
			t.Fatalf("lookup PID = %d; want live service %d", pid, svc.PID)
		}
		return processStartTimeWithNamespace(ctx, pid,
			func() error {
				return checkLinuxProcessNamespace(42, func() ([]byte, error) {
					return []byte("142 (controller) S"), nil
				})
			},
			func(context.Context, int) (time.Time, error) {
				queryCalled = true
				// The unrelated procfs PID could appear to match the record.
				return want, nil
			})
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("mismatched namespace reconciliation error = %v", err)
	}
	if queryCalled {
		t.Fatal("queried a target PID in an incompatible procfs view")
	}
	if !isPIDAlive(svc.PID) {
		t.Fatal("ownership refusal signaled the live service")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("ownership refusal removed its unresolved PID record: %v", err)
	}
}

func TestServiceMaxLifetime(t *testing.T) {
	serverBin := getTestServerBinary(t)
	dir := t.TempDir()

	profile := Profile{
		Name: "test-lifetime",
		Services: []ServiceSpec{
			{
				ID:             "srv-lifetime",
				Argv:           []string{serverBin},
				StartupTimeout: 5 * time.Second,
				MaxLifetime:    200 * time.Millisecond,
				ReadinessProbe: ReadinessProbe{
					Kind: "tcp_port",
				},
				PortConfig: PortConfig{
					Mode:       PortHandoffEnvVar,
					EnvVarName: "PORT",
				},
			},
		},
	}

	active, err := StartServices(context.Background(), profile.Services, dir, nil, "run_lifetime")
	if err != nil {
		t.Fatalf("StartServices: %v", err)
	}
	defer active.Teardown(context.Background())

	// Wait past MaxLifetime
	time.Sleep(350 * time.Millisecond)

	err = active.CheckHealth()
	if err == nil {
		t.Fatal("Expected CheckHealth to return error after exceeding MaxLifetime")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("Expected CategoryPolicyDenied for MaxLifetime violation, got: %v", err)
	}
}

func TestServiceReadinessVerification(t *testing.T) {
	serverBin := getTestServerBinary(t)
	dir := t.TempDir()

	// Service with expected body mismatch should fail readiness probe
	profile := Profile{
		Name: "test-probe-fail",
		Services: []ServiceSpec{
			{
				ID:             "srv-probe-fail",
				Argv:           []string{serverBin},
				StartupTimeout: 300 * time.Millisecond,
				ReadinessProbe: ReadinessProbe{
					Kind:         "http_get",
					Path:         "/health",
					ExpectedBody: "nonexistent_body_string",
					Interval:     20 * time.Millisecond,
				},
				PortConfig: PortConfig{
					Mode:       PortHandoffEnvVar,
					EnvVarName: "PORT",
				},
			},
		},
	}

	_, err := StartServices(context.Background(), profile.Services, dir, nil, "run_probe_fail")
	if err == nil {
		t.Fatal("Expected readiness probe to fail with timeout")
	}
	if errs.CategoryOf(err) != errs.CategoryProbeTimeout {
		t.Errorf("Expected CategoryProbeTimeout, got: %v", err)
	}
}

// TestAllocateUnusedPortRetriesConflictingPorts pins the pre-flight conflict
// branches that otherwise run only when an unrelated listener happens to hold
// the chosen port, which made whole-module coverage nondeterministic.
func TestAllocateUnusedPortRetriesConflictingPorts(t *testing.T) {
	probes := 0
	port, err := allocateUnusedPortWith(func(int) bool { probes++; return probes == 1 })
	if err != nil || port <= 0 || probes != 2 {
		t.Fatalf("port = %d, err = %v after %d probes; one conflict must be retried", port, err, probes)
	}
	probes = 0
	if _, err := allocateUnusedPortWith(func(int) bool { probes++; return true }); err == nil || probes != 3 {
		t.Fatalf("err = %v after %d probes; persistent conflict must fail after 3 attempts", err, probes)
	}
}

// TestPortRespondsSeesOnlyAcceptingPorts pins both outcomes of the pre-flight
// probe against a real loopback listener and a port that was just closed.
func TestPortRespondsSeesOnlyAcceptingPorts(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if !portResponds(port) {
		t.Fatalf("portResponds(%d) = false while a listener accepts on it", port)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if portResponds(port) {
		t.Fatalf("portResponds(%d) = true after its listener closed", port)
	}
}

// TestProbeHTTPReportsUnreachableServices pins the connection-failure branch
// against a port that was just closed.
func TestProbeHTTPReportsUnreachableServices(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if probeHTTP(port, "/", 0, "") {
		t.Fatalf("probeHTTP succeeded against closed port %d", port)
	}
}
