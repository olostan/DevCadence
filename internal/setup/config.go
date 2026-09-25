package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ManagedConfig is the whole managed-config key/value set, persisted at
// $DEVCADENCE_HOME/state/config.json. Keys and values are restricted to
// protocol.ManagedConfigKey's allowlist and ValidateValue rules — this file
// never holds anything write_managed_config's own type contract doesn't
// already allow.
type ManagedConfig map[protocol.ManagedConfigKey]string

func configPath(home string) string {
	return filepath.Join(home, "state", "config.json")
}

// ReadManagedConfig loads the managed config, returning an empty (not nil)
// map if the file does not exist yet.
func ReadManagedConfig(home string) (ManagedConfig, error) {
	path := configPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ManagedConfig{}, nil
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "read managed config %s", path)
	}
	var cfg ManagedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errs.Wrap(errs.CategoryIntegrity, err, "decode managed config %s", path)
	}
	if cfg == nil {
		cfg = ManagedConfig{}
	}
	return cfg, nil
}

// writeManagedConfigKey validates value against key's rules, then
// atomically persists key=value into the managed config file (read-modify-
// write of the whole small file — there is no concurrent-writer scenario
// this needs to handle beyond what AcquireExecutionLock already serializes
// at the whole-setup-run level). Unexported: the executor is the only
// intended path to mutating managed config (ADR-0014 §1/§7 MUST).
func writeManagedConfigKey(home string, key protocol.ManagedConfigKey, value string) error {
	if !key.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "writeManagedConfigKey: invalid key %q", key)
	}
	if err := key.ValidateValue(value); err != nil {
		return err
	}
	cfg, err := ReadManagedConfig(home)
	if err != nil {
		return err
	}
	cfg[key] = value
	return writeManagedConfigAtomic(home, cfg)
}

func writeManagedConfigAtomic(home string, cfg ManagedConfig) error {
	stateDir := filepath.Join(home, "state")
	if err := ensureDirMode(stateDir, 0700); err != nil {
		return err
	}
	path := configPath(home)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "marshal managed config")
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create temp managed config %s", tmpPath)
	}
	_, writeErr := tmpFile.Write(data)
	syncErr := tmpFile.Sync()
	closeErr := tmpFile.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, writeErr, "write temp managed config")
	}
	if syncErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, syncErr, "sync temp managed config")
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, closeErr, "close temp managed config")
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "commit managed config %s", path)
	}
	if err := ensureFileMode(path, 0600); err != nil {
		return err
	}

	if dirFile, err := os.Open(stateDir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
