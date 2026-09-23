package validation

import (
	"context"
	"encoding/json"
	"fmt"
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

