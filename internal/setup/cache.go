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
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	expiresAt := c.clock.Now().Add(ttl)
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

// profilePath returns the file path for an immutable cached MachineCapabilityProfile.
func (c *CacheManager) profilePath(profileID string) (string, error) {
	if profileID == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "setup cache: profile_id is required")
	}
	if filepath.Base(profileID) != profileID {
		return "", errs.New(errs.CategoryInvalidArgument, "setup cache: invalid profile_id %q", profileID)
	}
	return filepath.Join(c.rootDir, "profiles", profileID+".json"), nil
}

// WriteProfile atomically serializes an immutable MachineCapabilityProfile indexed
// by its ProfileID under a dedicated profiles/ subdirectory, and also updates the
// latest machine-profile target cache.
func WriteProfile(ctx context.Context, c *CacheManager, profile protocol.MachineCapabilityProfile, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	expiresAt := c.clock.Now().Add(ttl)
	return WriteProfileWithExpiresAt(ctx, c, profile, expiresAt)
}

// WriteProfileWithExpiresAt atomically archives an immutable MachineCapabilityProfile
// indexed by its ProfileID, and additionally updates the latest machine-profile
// target cache with the same expiration. Callers that must archive the exact
// active profile without refreshing the latest-cache TTL (e.g. a Doctor run
// deliberately retaining stale cached inference) should call archiveProfile
// directly instead.
func WriteProfileWithExpiresAt(ctx context.Context, c *CacheManager, profile protocol.MachineCapabilityProfile, expiresAt time.Time) error {
	if err := archiveProfile(ctx, c, profile, expiresAt); err != nil {
		return err
	}
	// Also update the latest machine-profile target cache for quick unkeyed lookup.
	return WriteWithExpiresAt(ctx, c, protocol.CacheTargetMachineProfile, profile.MachineFingerprint, profile, expiresAt)
}

// archiveProfile atomically writes profile into the immutable profiles/<profile_id>.json
// store only; it never touches the mutable "latest" machine-profile cache slot.
// A write for a ProfileID that is already archived with identical content is a
// no-op; a write with different content for the same ProfileID fails closed
// with CategoryConflict, since ProfileID is meant to be a stable content
// identity that every ResourceInventory.Profile reference can rely on.
func archiveProfile(ctx context.Context, c *CacheManager, profile protocol.MachineCapabilityProfile, expiresAt time.Time) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	now := c.clock.Now()

	profilesDir := filepath.Join(c.rootDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create profiles cache dir %s", profilesDir)
	}

	path, err := c.profilePath(profile.ProfileID)
	if err != nil {
		return err
	}

	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "open profile cache lock file %s", lockPath)
	}
	defer lockFile.Close()

	if err := lockExclusive(lockFile); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "lock profile cache lock file %s", lockPath)
	}
	defer unlock(lockFile)

	// The profiles/ store is immutable by ProfileID: once a path for a
	// ProfileID exists, this function may EITHER prove the new write is
	// identical (idempotent no-op) OR refuse to touch it — it must never
	// fall through to overwriting merely because the existing entry could
	// not be proven identical (a malformed file, an unparseable envelope,
	// content that fails MachineCapabilityProfile.Validate, or a decoded
	// ProfileID that doesn't match this path are all "cannot prove
	// identical," not "safe to replace" — independent-review follow-up on
	// WP-M3B-5, round-3 finding 1c and round-4 finding 3's tightening of
	// it).
	existingBytes, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		var existingEnv CacheEnvelope[protocol.MachineCapabilityProfile]
		if jsonErr := json.Unmarshal(existingBytes, &existingEnv); jsonErr != nil {
			return errs.New(errs.CategoryConflict,
				"setup cache: profile_id %q already has an unparseable archive entry at %s; refusing to overwrite", profile.ProfileID, path)
		}
		// Validate the whole existing envelope, not only its embedded Data:
		// a matching Data payload under a malformed envelope (e.g. an
		// invalid schema_version, or a fingerprint that disagrees with the
		// Data it wraps) would let this function report idempotent success
		// for an archive entry ReadProfileByID/ReadProfileByRef would then
		// reject — a resolvable-on-write, unresolvable-on-read
		// contradiction (independent-review follow-up on WP-M3B-5,
		// round-5 finding 2b).
		if existingEnv.SchemaVersion != protocol.SchemaVersion1 {
			return errs.New(errs.CategoryConflict,
				"setup cache: profile_id %q's archive envelope has invalid schema_version %q; refusing to treat as idempotent", profile.ProfileID, existingEnv.SchemaVersion)
		}
		if valErr := existingEnv.Data.Validate(); valErr != nil {
			return errs.New(errs.CategoryConflict,
				"setup cache: profile_id %q already has an invalid archived profile at %s (%v); refusing to overwrite", profile.ProfileID, path, valErr)
		}
		if existingEnv.Data.ProfileID != profile.ProfileID {
			return errs.New(errs.CategoryConflict,
				"setup cache: profile_id %q's archive file contains profile_id %q instead; refusing to overwrite", profile.ProfileID, existingEnv.Data.ProfileID)
		}
		if existingEnv.MachineFingerprint != existingEnv.Data.MachineFingerprint {
			return errs.New(errs.CategoryConflict,
				"setup cache: profile_id %q's archive envelope fingerprint %q disagrees with its own Data.MachineFingerprint %q; refusing to treat as idempotent",
				profile.ProfileID, existingEnv.MachineFingerprint, existingEnv.Data.MachineFingerprint)
		}
		existingCanonical, existingCanonErr := protocol.CanonicalJSON(existingEnv.Data)
		if existingCanonErr != nil {
			return errs.Wrap(errs.CategoryInternal, existingCanonErr, "canonicalize existing archived profile %s", profile.ProfileID)
		}
		newCanonical, newCanonErr := protocol.CanonicalJSON(profile)
		if newCanonErr != nil {
			return errs.Wrap(errs.CategoryInternal, newCanonErr, "canonicalize profile %s", profile.ProfileID)
		}
		if string(existingCanonical) == string(newCanonical) {
			return nil
		}
		return errs.New(errs.CategoryConflict,
			"setup cache: profile_id %q already archived with different content; ProfileID must be a stable content identity", profile.ProfileID)
	case errors.Is(readErr, fs.ErrNotExist):
		// No existing entry: proceed to create it below.
	default:
		return errs.Wrap(errs.CategoryInternal, readErr, "read existing profile cache file %s", path)
	}

	env := CacheEnvelope[protocol.MachineCapabilityProfile]{
		SchemaVersion:      protocol.SchemaVersion1,
		CreatedAt:          protocol.NewTimestamp(now),
		ExpiresAt:          protocol.NewTimestamp(expiresAt),
		MachineFingerprint: profile.MachineFingerprint,
		Data:               profile,
	}

	bytes, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "marshal profile cache envelope")
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", path, now.UnixNano())
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create temp profile cache file %s", tmpPath)
	}

	_, writeErr := tmpFile.Write(bytes)
	syncErr := tmpFile.Sync()
	closeErr := tmpFile.Close()

	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, writeErr, "write temp profile cache file")
	}
	if syncErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, syncErr, "sync temp profile cache file")
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, closeErr, "close temp profile cache file")
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "commit profile cache file %s", path)
	}

	if dirFile, err := os.Open(profilesDir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

// ReadProfileByID loads an immutable MachineCapabilityProfile by its unique ProfileID.
// If missing or corrupted, it returns (zero, false, nil).
func ReadProfileByID(ctx context.Context, c *CacheManager, profileID string) (protocol.MachineCapabilityProfile, bool, error) {
	var zero protocol.MachineCapabilityProfile
	path, err := c.profilePath(profileID)
	if err != nil {
		return zero, false, err
	}

	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, false, nil
		}
		return zero, false, errs.Wrap(errs.CategoryInternal, err, "open profile cache lock file %s", lockPath)
	}
	defer lockFile.Close()

	if err := lockShared(lockFile); err != nil {
		return zero, false, errs.Wrap(errs.CategoryInternal, err, "lock profile cache lock file %s", lockPath)
	}
	defer unlock(lockFile)

	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, false, nil
		}
		return zero, false, errs.Wrap(errs.CategoryInternal, err, "open profile cache file %s", path)
	}
	defer f.Close()

	// This is the immutable provenance archive, not the disposable mutable
	// cache: a read that finds corrupt/invalid content must fail closed
	// (not-found, or an integrity error) without deleting anything.
	// Deleting on read would let a later archiveProfile call see a missing
	// path and freely create different bytes under the same ProfileID —
	// exactly the replacement the immutable-key design exists to prohibit
	// (independent-review follow-up on WP-M3B-5, round-5 finding 2a).
	var env CacheEnvelope[protocol.MachineCapabilityProfile]
	dec := json.NewDecoder(f)
	if err := dec.Decode(&env); err != nil {
		return zero, false, nil
	}

	if env.SchemaVersion != protocol.SchemaVersion1 {
		return zero, false, nil
	}

	if err := env.Data.Validate(); err != nil {
		return zero, false, nil
	}

	// The file is keyed by ProfileID in its path, but the path alone is not
	// proof the stored payload actually is that profile (e.g. a corrupted
	// rename, a manually edited file, or a future bug in profilePath). Cross
	// check the loaded content's own ProfileID before returning it as "the"
	// profile for that ID (independent-review follow-up on WP-M3B-5,
	// finding 1c's resolver-validation ask).
	if env.Data.ProfileID != profileID {
		return zero, false, nil
	}

	return env.Data, true, nil
}

// ReadProfileByRef resolves a protocol.MachineProfileRef to its full archived
// MachineCapabilityProfile, cross-checking every field the reference
// carries (ProfileID, MachineFingerprint, ObservedAt, ProbeDepth) against
// the loaded content — not only the ProfileID ReadProfileByID alone checks.
// A consumer resolving a durable ResourceInventory.Profile reference should
// prefer this over ReadProfileByID so a future drift between the reference
// and the archive (e.g. two different observations that happened to share
// a ProfileID, or a reference that predates an archive schema change) is
// caught rather than silently ignored (independent-review follow-up on
// WP-M3B-5, round-4 finding 3).
func ReadProfileByRef(ctx context.Context, c *CacheManager, ref protocol.MachineProfileRef) (protocol.MachineCapabilityProfile, bool, error) {
	profile, found, err := ReadProfileByID(ctx, c, ref.ProfileID)
	if err != nil || !found {
		return protocol.MachineCapabilityProfile{}, found, err
	}
	if profile.MachineFingerprint != ref.MachineFingerprint {
		return protocol.MachineCapabilityProfile{}, false, nil
	}
	if !profile.ObservedAt.Time().Equal(ref.ObservedAt.Time()) {
		return protocol.MachineCapabilityProfile{}, false, nil
	}
	if profile.ProbeDepth != ref.ProbeDepth {
		return protocol.MachineCapabilityProfile{}, false, nil
	}
	return profile, true, nil
}
