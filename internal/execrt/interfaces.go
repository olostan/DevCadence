package execrt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
)

// MaxArtifactBytes is the maximum artifact payload size permitted for storage (4 MiB).
const MaxArtifactBytes = 4 * 1024 * 1024

// RepositoryProvider resolves registered repositories by project ID.
type RepositoryProvider interface {
	Repository(ctx context.Context, projectID string) (*repository.Repository, error)
}

// ArtifactSink provides content-addressed, immutable artifact storage.
type ArtifactSink interface {
	Put(ctx context.Context, projectID, kind, mediaType string, content []byte) (protocol.ArtifactRef, error)
}

// MemoryArtifactSink is an in-memory implementation of ArtifactSink for testing.
type MemoryArtifactSink struct {
	mu     sync.RWMutex
	stored map[string][]byte
	refs   map[string]protocol.ArtifactRef
}

// NewMemoryArtifactSink creates an in-memory artifact sink.
func NewMemoryArtifactSink() *MemoryArtifactSink {
	return &MemoryArtifactSink{
		stored: make(map[string][]byte),
		refs:   make(map[string]protocol.ArtifactRef),
	}
}

// Put stores content in memory if it does not exceed MaxArtifactBytes.
// Returns errs.CategoryInvalidArgument if content exceeds 4 MiB.
func (m *MemoryArtifactSink) Put(ctx context.Context, projectID, kind, mediaType string, content []byte) (protocol.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ArtifactRef{}, err
	}
	if len(content) > MaxArtifactBytes {
		return protocol.ArtifactRef{}, errs.New(
			errs.CategoryInvalidArgument,
			"artifact size %d exceeds maximum allowed size %d bytes",
			len(content),
			MaxArtifactBytes,
		)
	}

	digest := protocol.DigestBytes(content)
	rawHex := strings.TrimPrefix(digest, "sha256:")
	id := fmt.Sprintf("art-%s", rawHex[:16])
	locator := fmt.Sprintf("memory://%s/%s", projectID, rawHex)

	ref := protocol.ArtifactRef{
		ID:        id,
		Kind:      kind,
		Locator:   locator,
		MediaType: mediaType,
		Digest:    digest,
		SizeBytes: int64(len(content)),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stored == nil {
		m.stored = make(map[string][]byte)
	}
	if m.refs == nil {
		m.refs = make(map[string]protocol.ArtifactRef)
	}

	cp := make([]byte, len(content))
	copy(cp, content)
	m.stored[ref.Locator] = cp
	m.refs[ref.Locator] = ref

	return ref, nil
}

// Get retrieves content stored under the locator.
func (m *MemoryArtifactSink) Get(locator string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.stored == nil {
		return nil, false
	}
	b, ok := m.stored[locator]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, true
}

// SingleRepositoryProvider implements RepositoryProvider given a single instance
// or a map of project IDs to repositories, returning errs.CategoryNotFound for unmapped projects.
type SingleRepositoryProvider struct {
	Repo  *repository.Repository
	Repos map[string]*repository.Repository
}

// NewSingleRepositoryProvider wraps a single repository instance.
func NewSingleRepositoryProvider(repo *repository.Repository) *SingleRepositoryProvider {
	p := &SingleRepositoryProvider{
		Repo: repo,
	}
	if repo != nil && repo.ProjectID != "" {
		p.Repos = map[string]*repository.Repository{
			repo.ProjectID: repo,
		}
	}
	return p
}

// NewMapRepositoryProvider wraps a map of repositories keyed by project ID.
func NewMapRepositoryProvider(repos map[string]*repository.Repository) *SingleRepositoryProvider {
	cp := make(map[string]*repository.Repository, len(repos))
	for k, v := range repos {
		cp[k] = v
	}
	return &SingleRepositoryProvider{
		Repos: cp,
	}
}

// Repository returns the repository associated with projectID or CategoryNotFound.
func (p *SingleRepositoryProvider) Repository(ctx context.Context, projectID string) (*repository.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || projectID == "" {
		return nil, errs.New(errs.CategoryNotFound, "project %q repository not found", projectID)
	}
	if p.Repos != nil {
		if r, ok := p.Repos[projectID]; ok && r != nil {
			return r, nil
		}
	}
	if p.Repo != nil && p.Repo.ProjectID == projectID {
		return p.Repo, nil
	}
	return nil, errs.New(errs.CategoryNotFound, "project %q repository not found", projectID)
}

// DirectoryArtifactSink stores artifacts in a local filesystem directory.
type DirectoryArtifactSink struct {
	BaseDir string
}

// NewDirectoryArtifactSink creates an artifact sink backed by baseDir.
func NewDirectoryArtifactSink(baseDir string) *DirectoryArtifactSink {
	return &DirectoryArtifactSink{BaseDir: baseDir}
}

// Put writes the content to disk under BaseDir/<projectID>/artifacts/<digest-prefix>/<digest>.
func (s *DirectoryArtifactSink) Put(ctx context.Context, projectID, kind, mediaType string, content []byte) (protocol.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ArtifactRef{}, err
	}
	if len(content) > MaxArtifactBytes {
		return protocol.ArtifactRef{}, errs.New(
			errs.CategoryInvalidArgument,
			"artifact size %d exceeds maximum allowed size %d bytes",
			len(content),
			MaxArtifactBytes,
		)
	}
	if strings.TrimSpace(s.BaseDir) == "" {
		return protocol.ArtifactRef{}, errs.New(errs.CategoryInvalidArgument, "baseDir is required")
	}

	digest := protocol.DigestBytes(content)
	rawHex := strings.TrimPrefix(digest, "sha256:")
	id := fmt.Sprintf("art-%s", rawHex[:16])

	dir := filepath.Join(s.BaseDir, projectID, "artifacts", rawHex[:2])
	if err := os.MkdirAll(dir, 0700); err != nil {
		return protocol.ArtifactRef{}, errs.Wrap(errs.CategoryInternal, err, "failed to create artifact directory")
	}

	filePath := filepath.Join(dir, rawHex)
	if err := os.WriteFile(filePath, content, 0600); err != nil {
		return protocol.ArtifactRef{}, errs.Wrap(errs.CategoryInternal, err, "failed to write artifact file")
	}

	return protocol.ArtifactRef{
		ID:        id,
		Kind:      kind,
		Locator:   filePath,
		MediaType: mediaType,
		Digest:    digest,
		SizeBytes: int64(len(content)),
	}, nil
}
