package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DefaultCacheTTL is the standard lifetime of cached machine capability data (24 hours).
const DefaultCacheTTL = 24 * time.Hour

// CacheEnvelope wraps cached operational objects with validation metadata.
type CacheEnvelope[T any] struct {
	SchemaVersion      string             `json:"schema_version"`
	CreatedAt          protocol.Timestamp `json:"created_at"`
	ExpiresAt          protocol.Timestamp `json:"expires_at"`
	MachineFingerprint string             `json:"machine_fingerprint"`
	Data               T                  `json:"data"`
}

// CacheManager manages cached operational objects under a designated root directory.
type CacheManager struct {
	rootDir    string
	clock      clock.Clock
	defaultTTL time.Duration
}

// NewCacheManager returns a CacheManager.
func NewCacheManager(rootDir string, clk clock.Clock, defaultTTL time.Duration) (*CacheManager, error) {
	if rootDir == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "setup cache: root directory is required")
	}
	if clk == nil {
		clk = clock.System()
	}
	if defaultTTL <= 0 {
		defaultTTL = DefaultCacheTTL
	}
	return &CacheManager{
		rootDir:    rootDir,
		clock:      clk,
		defaultTTL: defaultTTL,
	}, nil
}

// targetFilename maps an allowlisted CacheTarget to its relative file name.
func (c *CacheManager) targetFilename(target protocol.CacheTarget) (string, error) {
	if !target.Valid() {
		return "", errs.New(errs.CategoryInvalidArgument, "setup cache: invalid target %q", target)
	}
	switch target {
	case protocol.CacheTargetMachineProfile:
		return "machine-profile.json", nil
	case protocol.CacheTargetEndpointProbes:
		return "endpoint-probes.json", nil
	default:
		return "", errs.New(errs.CategoryInvalidArgument, "setup cache: unhandled target %q", target)
	}
}

// targetPath returns the full filesystem path for a cache target.
func (c *CacheManager) targetPath(target protocol.CacheTarget) (string, error) {
	filename, err := c.targetFilename(target)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.rootDir, filename), nil
}

// ReadEntry loads and validates a cached object, returning whether it was found
// ReadEnvelope loads and validates a cached object along with its envelope.
// Unlike Read, ReadEnvelope does NOT delete expired cache files,
// allowing callers to inspect stale evidence when fresh probes are shallower.
// If the cache is missing or corrupted, it returns (zero, false, false, nil).
// If fingerprint does not match, it returns (zero, false, false, nil).
// If expired, it returns (env, true, true, nil).
// If fresh and valid, it returns (env, true, false, nil).
func ReadEnvelope[T any](ctx context.Context, c *CacheManager, target protocol.CacheTarget, expectedFingerprint string) (CacheEnvelope[T], bool, bool, error) {
	var zero CacheEnvelope[T]
	path, err := c.targetPath(target)
	if err != nil {
		return zero, false, false, err
	}

	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, false, false, nil
		}
		return zero, false, false, errs.Wrap(errs.CategoryInternal, err, "open cache lock file %s", lockPath)
	}
	defer lockFile.Close()

	if err := lockShared(lockFile); err != nil {
		return zero, false, false, errs.Wrap(errs.CategoryInternal, err, "lock cache lock file %s", lockPath)
	}
	defer unlock(lockFile)

	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, false, false, nil
		}
		return zero, false, false, errs.Wrap(errs.CategoryInternal, err, "open cache file %s", path)
	}
	defer f.Close()

	var env CacheEnvelope[T]
	dec := json.NewDecoder(f)
	if err := dec.Decode(&env); err != nil {
		// Corrupted cache file: drop it silently
		_ = os.Remove(path)
		return zero, false, false, nil
	}

	if env.SchemaVersion != protocol.SchemaVersion1 {
		_ = os.Remove(path)
		return zero, false, false, nil
	}

	if expectedFingerprint != "" && env.MachineFingerprint != expectedFingerprint {
		return zero, false, false, nil
	}

	now := c.clock.Now()
	expired := !env.ExpiresAt.Time().IsZero() && !now.Before(env.ExpiresAt.Time())

	return env, true, expired, nil
}

// ReadEntry loads and validates a cached object, returning whether it was found
// and whether it is expired.
func ReadEntry[T any](ctx context.Context, c *CacheManager, target protocol.CacheTarget, expectedFingerprint string) (T, bool, bool, error) {
	env, found, expired, err := ReadEnvelope[T](ctx, c, target, expectedFingerprint)
	return env.Data, found, expired, err
}

// Read loads and validates a cached object. If the cache is missing, expired,
// corrupted, or belongs to a different machine fingerprint, it returns (zero, false, nil).
func Read[T any](ctx context.Context, c *CacheManager, target protocol.CacheTarget, expectedFingerprint string) (T, bool, error) {
	data, found, expired, err := ReadEntry[T](ctx, c, target, expectedFingerprint)
	if err != nil || !found || expired {
		var zero T
		return zero, false, err
	}
	return data, true, nil
}

// Write atomically serialises data into the target cache file with a given TTL.
func Write[T any](ctx context.Context, c *CacheManager, target protocol.CacheTarget, fingerprint string, data T, ttl time.Duration) error {
	now := c.clock.Now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	return WriteWithExpiresAt(ctx, c, target, fingerprint, data, expiresAt)
}

// WriteWithExpiresAt atomically serialises data into the target cache file with an explicit expiration.
func WriteWithExpiresAt[T any](ctx context.Context, c *CacheManager, target protocol.CacheTarget, fingerprint string, data T, expiresAt time.Time) error {
	path, err := c.targetPath(target)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(c.rootDir, 0700); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create cache root directory %s", c.rootDir)
	}

	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "open cache lock file %s", lockPath)
	}
	defer lockFile.Close()

	if err := lockExclusive(lockFile); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "lock cache lock file %s", lockPath)
	}
	defer unlock(lockFile)

	now := c.clock.Now()
	env := CacheEnvelope[T]{
		SchemaVersion:      protocol.SchemaVersion1,
		CreatedAt:          protocol.NewTimestamp(now),
		ExpiresAt:          protocol.NewTimestamp(expiresAt),
		MachineFingerprint: fingerprint,
		Data:               data,
	}

	bytes, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "marshal cache envelope")
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", path, now.UnixNano())
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create temp cache file %s", tmpPath)
	}

	_, writeErr := tmpFile.Write(bytes)
	syncErr := tmpFile.Sync()
	closeErr := tmpFile.Close()

	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, writeErr, "write temp cache file")
	}
	if syncErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, syncErr, "sync temp cache file")
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, closeErr, "close temp cache file")
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "commit cache file %s", path)
	}

	// Durably sync directory entry
	if dirFile, err := os.Open(c.rootDir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

// Remove evicts a cached target file under an exclusive file lock.
func (c *CacheManager) Remove(ctx context.Context, target protocol.CacheTarget) error {
	path, err := c.targetPath(target)
	if err != nil {
		return err
	}
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err == nil {
		_ = lockExclusive(lockFile)
		defer func() {
			_ = unlock(lockFile)
			_ = lockFile.Close()
		}()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errs.Wrap(errs.CategoryInternal, err, "remove cache file %s", path)
	}
	if dirFile, err := os.Open(c.rootDir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
