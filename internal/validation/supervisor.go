package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	DefaultStartupTimeout  = 30 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
	DefaultMaxLifetime     = 15 * time.Minute
	DefaultProbeInterval   = 50 * time.Millisecond
)

// PIDRecord represents process metadata persisted for crash reconciliation (ADR-0016).
type PIDRecord struct {
	PID        int       `json:"pid"`
	StartTime  time.Time `json:"start_time"`
	Executable string    `json:"executable"`
	ServiceID  string    `json:"service_id"`
}

// ActiveService represents a running supervised service.
type ActiveService struct {
	Spec         ServiceSpec
	PID          int
	Port         int
	TempDir      string
	cmd          *exec.Cmd
	doneCh       chan struct{}
	exitErr      error
	startedAt    time.Time
	mu           sync.RWMutex
	exceededLife bool
	lifetimeDone chan struct{}
}

// ActiveServices manages all active services for a validation run.
type ActiveServices struct {
	mu       sync.Mutex
	services []*ActiveService
	env      []string
	runID    string
}

// Env returns the environment variables contributed by active services (e.g. PORT=...).
func (a *ActiveServices) Env() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.env...)
}

// CheckHealth verifies that no supervised service has died prematurely or exceeded its lifetime.
func (a *ActiveServices) CheckHealth() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, s := range a.services {
		s.mu.RLock()
		exceeded := s.exceededLife
		s.mu.RUnlock()

		if exceeded {
			return errs.New(errs.CategoryPolicyDenied, "service %q exceeded maximum lifetime of %v", s.Spec.ID, s.Spec.MaxLifetime)
		}

		select {
		case <-s.doneCh:
			if s.exitErr != nil {
				return errs.Wrap(errs.CategoryInternal, s.exitErr, "service %q died prematurely", s.Spec.ID)
			}
			return errs.New(errs.CategoryInternal, "service %q exited unexpectedly with code 0", s.Spec.ID)
		default:
		}
	}
	return nil
}

// Monitor watches active services in the background during check execution.
// If any service exits unexpectedly, it cancels the check context with the failure cause.
func (a *ActiveServices) Monitor(ctx context.Context, cancel context.CancelCauseFunc) func() {
	if a == nil {
		return func() {}
	}

	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(stopCh)
		})
	}

	a.mu.Lock()
	services := append([]*ActiveService(nil), a.services...)
	a.mu.Unlock()

	for _, s := range services {
		go func(svc *ActiveService) {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			case <-svc.doneCh:
				err := a.CheckHealth()
				if err == nil {
					err = errs.New(errs.CategoryInternal, "service %q exited unexpectedly", svc.Spec.ID)
				}
				cancel(err)
			}
		}(s)
	}

	return stop
}

// Teardown stops and reaps all active services, cleaning up temp directories.
func (a *ActiveServices) Teardown(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var errsList []string
	for _, s := range a.services {
		if s.cmd != nil && s.cmd.Process != nil {
			close(s.lifetimeDone)

			// Graceful SIGTERM
			killServiceGraceful(s.cmd.Process)

			shutdownTimeout := s.Spec.ShutdownTimeout
			if shutdownTimeout <= 0 {
				shutdownTimeout = DefaultShutdownTimeout
			}

			select {
			case <-s.doneCh:
				// Exited gracefully
			case <-time.After(shutdownTimeout):
				// Forced SIGKILL
				killServiceForced(s.cmd.Process)
				select {
				case <-s.doneCh:
				case <-time.After(2 * time.Second):
					errsList = append(errsList, fmt.Sprintf("service %q failed to terminate after SIGKILL", s.Spec.ID))
				}
			}
		}

		if s.TempDir != "" {
			_ = os.RemoveAll(s.TempDir)
		}
	}

	a.services = nil
	if len(errsList) > 0 {
		return errs.New(errs.CategoryInternal, "teardown errors: %s", strings.Join(errsList, "; "))
	}
	return nil
}

// StartServices boots, probes, and supervises background services for a profile.
func StartServices(ctx context.Context, specs []ServiceSpec, baseDir string, baseEnv []string, runID string, modules ...[]protocol.ModuleDefinition) (*ActiveServices, error) {
	if runID == "" {
		runID = fmt.Sprintf("val_%d", time.Now().UnixNano())
	}

	active := &ActiveServices{
		runID: runID,
		env:   append([]string(nil), baseEnv...),
	}

	var mods []protocol.ModuleDefinition
	if len(modules) > 0 {
		mods = modules[0]
	}

	for _, spec := range specs {
		svc, err := startSingleService(ctx, spec, baseDir, active.env, runID, mods)
		if err != nil {
			// Teardown already started services
			_ = active.Teardown(context.Background())
			return nil, err
		}
		active.services = append(active.services, svc)

		// If port assigned and EnvVarName is specified, propagate to subsequent services/checks
		if svc.Port > 0 && spec.PortConfig.EnvVarName != "" {
			active.env = append(active.env, fmt.Sprintf("%s=%d", spec.PortConfig.EnvVarName, svc.Port))
		}
	}

	return active, nil
}

func startSingleService(ctx context.Context, spec ServiceSpec, baseDir string, currentEnv []string, runID string, modules []protocol.ModuleDefinition) (*ActiveService, error) {
	workingDir := baseDir
	if spec.ModuleID != "" {
		var foundMod *protocol.ModuleDefinition
		for i := range modules {
			if modules[i].ID == spec.ModuleID {
				foundMod = &modules[i]
				break
			}
		}
		if foundMod == nil {
			return nil, errs.New(errs.CategoryInvalidArgument, "validation: service %q module %q not found in project modules catalog", spec.ID, spec.ModuleID)
		}
		modRoot, err := ValidateDirContainment(baseDir, foundMod.Path)
		if err != nil {
			return nil, err
		}
		workingDir = modRoot
	}
	if spec.Dir != "" {
		contained, err := ValidateDirContainment(workingDir, spec.Dir)
		if err != nil {
			return nil, err
		}
		workingDir = contained
	}

	tempDir, err := os.MkdirTemp("", fmt.Sprintf("devcadence_val_%s_%s_*", runID, spec.ID))
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "create service temp dir")
	}

	argv := append([]string(nil), spec.Argv...)
	serviceEnv := append([]string(nil), currentEnv...)
	for k, v := range spec.Env {
		serviceEnv = append(serviceEnv, fmt.Sprintf("%s=%s", k, v))
	}

	var port int
	var listener *net.TCPListener
	var listenerFile *os.File

	switch spec.PortConfig.Mode {
	case PortHandoffSocketInheritance:
		l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, errs.Wrap(errs.CategoryInternal, err, "bind socket inheritance listener")
		}
		listener = l
		port = l.Addr().(*net.TCPAddr).Port
		f, err := l.File()
		if err != nil {
			_ = l.Close()
			_ = os.RemoveAll(tempDir)
			return nil, errs.Wrap(errs.CategoryInternal, err, "obtain listener file descriptor")
		}
		listenerFile = f
		if spec.PortConfig.EnvVarName != "" {
			serviceEnv = append(serviceEnv, fmt.Sprintf("%s=%d", spec.PortConfig.EnvVarName, port))
		}

	case PortHandoffCLIFlag:
		p, err := allocateUnusedPort()
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
		port = p
		flagTmpl := spec.PortConfig.FlagTemplate
		if flagTmpl == "" {
			flagTmpl = "--port=%d"
		}
		argv = append(argv, fmt.Sprintf(flagTmpl, port))
		if spec.PortConfig.EnvVarName != "" {
			serviceEnv = append(serviceEnv, fmt.Sprintf("%s=%d", spec.PortConfig.EnvVarName, port))
		}

	case PortHandoffEnvVar:
		p, err := allocateUnusedPort()
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
		port = p
		envVar := spec.PortConfig.EnvVarName
		if envVar == "" {
			envVar = "PORT"
		}
		serviceEnv = append(serviceEnv, fmt.Sprintf("%s=%d", envVar, port))
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workingDir
	cmd.Env = serviceEnv
	setServiceProcAttrs(cmd)

	if listenerFile != nil {
		cmd.ExtraFiles = []*os.File{listenerFile}
	}

	startedAt := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		if listenerFile != nil {
			_ = listenerFile.Close()
		}
		if listener != nil {
			_ = listener.Close()
		}
		_ = os.RemoveAll(tempDir)
		return nil, errs.Wrap(errs.CategoryInternal, err, "start service %q", spec.ID)
	}

	// Close parent copy of listener file descriptor
	if listenerFile != nil {
		_ = listenerFile.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}

	pidRecord := PIDRecord{
		PID:        cmd.Process.Pid,
		StartTime:  startedAt,
		Executable: argv[0],
		ServiceID:  spec.ID,
	}
	pidData, _ := json.Marshal(pidRecord)
	_ = os.WriteFile(filepath.Join(tempDir, "service.pid"), pidData, 0o600)

	doneCh := make(chan struct{})
	lifetimeDone := make(chan struct{})

	activeSvc := &ActiveService{
		Spec:         spec,
		PID:          cmd.Process.Pid,
		Port:         port,
		TempDir:      tempDir,
		cmd:          cmd,
		doneCh:       doneCh,
		startedAt:    startedAt,
		lifetimeDone: lifetimeDone,
	}

	go func() {
		activeSvc.exitErr = cmd.Wait()
		close(doneCh)
	}()

	// MaxLifetime timer
	maxLifetime := spec.MaxLifetime
	if maxLifetime <= 0 {
		maxLifetime = DefaultMaxLifetime
	}
	go func() {
		select {
		case <-time.After(maxLifetime):
			activeSvc.mu.Lock()
			activeSvc.exceededLife = true
			activeSvc.mu.Unlock()
			killServiceForced(cmd.Process)
		case <-lifetimeDone:
		case <-doneCh:
		}
	}()

	// Readiness probing
	startupTimeout := spec.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}

	if err := waitForReadiness(ctx, activeSvc, startupTimeout); err != nil {
		killServiceForced(cmd.Process)
		<-doneCh
		_ = os.RemoveAll(tempDir)
		return nil, err
	}

	return activeSvc, nil
}

func allocateUnusedPort() (int, error) {
	for attempt := 0; attempt < 3; attempt++ {
		l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			return 0, errs.Wrap(errs.CategoryInternal, err, "allocate ephemeral port")
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()

		// Pre-flight probe: ensure port is not already responding
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 20*time.Millisecond)
		if err == nil {
			// Port was responding; conflict with unrelated pre-existing service
			_ = conn.Close()
			continue
		}
		return port, nil
	}
	return 0, errs.New(errs.CategoryInternal, "failed to allocate free port after 3 attempts")
}

func waitForReadiness(ctx context.Context, svc *ActiveService, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	probe := svc.Spec.ReadinessProbe
	interval := probe.Interval
	if interval <= 0 {
		interval = DefaultProbeInterval
	}

	for {
		if time.Now().After(deadline) {
			return errs.New(errs.CategoryProbeTimeout, "service %q failed readiness probe within %v", svc.Spec.ID, timeout)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		select {
		case <-svc.doneCh:
			if svc.exitErr != nil {
				return errs.Wrap(errs.CategoryInternal, svc.exitErr, "service %q exited during startup", svc.Spec.ID)
			}
			return errs.New(errs.CategoryInternal, "service %q exited during startup with code 0", svc.Spec.ID)
		default:
		}

		var ok bool
		switch probe.Kind {
		case "http_get":
			ok = probeHTTP(svc.Port, probe.Path, probe.ExpectedStatus, probe.ExpectedBody)
		default: // "tcp_port" or empty
			ok = probeTCP(svc.Port)
		}

		if ok {
			return nil
		}

		time.Sleep(interval)
	}
}

func probeTCP(port int) bool {
	if port <= 0 {
		return true
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

func probeHTTP(port int, path string, expectedStatus int, expectedBody string) bool {
	if port <= 0 {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		return false
	}
	if expectedStatus == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 400) {
		return false
	}
	if expectedBody != "" {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil || !strings.Contains(string(bodyBytes), expectedBody) {
			return false
		}
	}
	return true
}

// VerifyProcessOwnership checks if a PID is alive and matches the recorded start time (ADR-0016).
func VerifyProcessOwnership(record PIDRecord) bool {
	if record.PID <= 0 {
		return false
	}

	// 1. Check if process exists
	if !isPIDAlive(record.PID) {
		return false
	}

	// 2. Query process start time via ps
	out, err := exec.Command("ps", "-p", strconv.Itoa(record.PID), "-o", "lstart=").Output()
	if err != nil {
		return false
	}

	str := strings.TrimSpace(string(out))
	if str == "" {
		return false
	}

	// Format: "Mon Jan _2 15:04:05 2006"
	parsed, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", str, time.Local)
	if err != nil {
		return false
	}

	// Compare with recorded start time (allow up to 2s drift due to ps resolution)
	diff := parsed.Sub(record.StartTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 2*time.Second
}

// ReconcileAndCleanup checks process ownership before terminating stale services.
// It refuses to signal recycled PIDs if start time does not match.
func ReconcileAndCleanup(pidFilePath string) error {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errs.Wrap(errs.CategoryInternal, err, "read pid file")
	}

	var record PIDRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "unmarshal pid record")
	}

	if !VerifyProcessOwnership(record) {
		return errs.New(errs.CategoryPolicyDenied,
			"unresolved_reconciliation: process %d ownership cannot be verified against start time %v (refusing to signal PID)",
			record.PID, record.StartTime)
	}

	// Ownership verified: safely terminate process group
	terminateProcessGroup(record.PID)
	_ = os.Remove(pidFilePath)

	return nil
}
