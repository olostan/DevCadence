package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
)

// RuleRegistry stores and validates the canonical corpus of normative rules.
type RuleRegistry struct {
	mu           sync.RWMutex
	rules        map[string]Rule
	knownDomains map[string]struct{}
	frozen       bool

	catalogID                 string
	catalogVersion            string
	sourceRevision            string
	mappingRevision           string
	normativeSourceDigest     string
	authorityProjectionDigest string
	catalogDigest             string
}

// CatalogID returns the frozen catalog identifier.
func (reg *RuleRegistry) CatalogID() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogID
}

// CatalogVersion returns the frozen catalog specification version.
func (reg *RuleRegistry) CatalogVersion() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogVersion
}

// SourceRevision returns the git commit revision of the normative source document (e.g. INVARIANTS.md).
func (reg *RuleRegistry) SourceRevision() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.sourceRevision
}

// MappingRevision returns the revision of the canonical invariant applicability mappings.
func (reg *RuleRegistry) MappingRevision() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.mappingRevision
}

// CatalogRevision returns the git commit revision the catalog is pinned to (backwards-compatible alias for SourceRevision).
func (reg *RuleRegistry) CatalogRevision() string {
	return reg.SourceRevision()
}

// NormativeSourceDigest returns the cryptographic SHA-256 digest of all verbatim normative clauses.
func (reg *RuleRegistry) NormativeSourceDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.normativeSourceDigest
}

// AuthorityProjectionDigest returns the cryptographic SHA-256 digest of all mapping semantics.
func (reg *RuleRegistry) AuthorityProjectionDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.authorityProjectionDigest
}

// MappingDigest is an alias for AuthorityProjectionDigest.
func (reg *RuleRegistry) MappingDigest() string {
	return reg.AuthorityProjectionDigest()
}

// CatalogDigest returns the cryptographic SHA-256 digest authenticating both normative source and authority projection.
func (reg *RuleRegistry) CatalogDigest() string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.catalogDigest
}

// SetCatalogMeta records catalog provenance metadata before the registry is frozen.
func (reg *RuleRegistry) SetCatalogMeta(id, version, sourceRevision, mappingRevision string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot set catalog meta")
	}
	reg.catalogID = id
	reg.catalogVersion = version
	reg.sourceRevision = sourceRevision
	reg.mappingRevision = mappingRevision
	return nil
}

// NewRuleRegistry creates an empty RuleRegistry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules:        make(map[string]Rule),
		knownDomains: make(map[string]struct{}),
	}
}

// RegisterKnownDomain adds a recognized domain identifier to the registry vocabulary.
func (reg *RuleRegistry) RegisterKnownDomain(domain string) error {
	clean := strings.ToLower(strings.TrimSpace(domain))
	if clean == "" {
		return errs.New(errs.CategoryInvalidArgument, "domain cannot be empty")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot register new domain")
	}
	reg.knownDomains[clean] = struct{}{}
	return nil
}

// IsDomainKnown reports whether the domain is part of the known domain vocabulary.
func (reg *RuleRegistry) IsDomainKnown(domain string) bool {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	clean := strings.ToLower(strings.TrimSpace(domain))
	_, ok := reg.knownDomains[clean]
	return ok
}

// Register adds a rule to the registry after computing its digest and validating constraints (deep copy on ingress).
func (reg *RuleRegistry) Register(r Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}

	// Compute or verify content digest
	hasher := sha256.New()
	hasher.Write([]byte(r.Content))
	expectedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if r.ContentDigest == "" {
		r.ContentDigest = expectedDigest
	} else if r.ContentDigest != expectedDigest {
		return errs.New(errs.CategoryInvalidArgument,
			"Rule %q: content_digest (%q) does not match sha256 of content (%q)", r.ID, r.ContentDigest, expectedDigest)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	if reg.frozen {
		return errs.New(errs.CategoryConflict, "RuleRegistry is frozen; cannot register new rule")
	}

	if _, exists := reg.rules[r.ID]; exists {
		return errs.New(errs.CategoryConflict, "Rule %q already registered", r.ID)
	}

	// Add domains to known domains set
	for _, d := range r.Domains {
		clean := strings.ToLower(strings.TrimSpace(d))
		if clean != "" {
			reg.knownDomains[clean] = struct{}{}
		}
	}

	reg.rules[r.ID] = r.deepCopy()
	return nil
}

// Get retrieves a rule by ID (deep copy on egress).
func (reg *RuleRegistry) Get(id string) (Rule, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	r, ok := reg.rules[id]
	if !ok {
		return Rule{}, false
	}
	return r.deepCopy(), true
}

// All returns all registered rules sorted deterministically by ID (deep copy on egress).
func (reg *RuleRegistry) All() []Rule {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	result := make([]Rule, 0, len(reg.rules))
	for _, r := range reg.rules {
		result = append(result, r.deepCopy())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Freeze validates reverse-coverage, computes deterministic cryptographic digests
// (NormativeSourceDigest, AuthorityProjectionDigest, CatalogDigest), and marks the registry immutable.
func (reg *RuleRegistry) Freeze() error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		return nil
	}
	if err := reg.validateReverseCoverageLocked(); err != nil {
		return err
	}

	ids := make([]string, 0, len(reg.rules))
	for id := range reg.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// 1. NormativeSourceDigest: authenticates exact normative clause text & content digests
	srcHasher := sha256.New()
	for _, id := range ids {
		r := reg.rules[id]
		srcHasher.Write([]byte(r.ID + ":" + r.ContentDigest + "\n"))
	}
	reg.normativeSourceDigest = "sha256:" + hex.EncodeToString(srcHasher.Sum(nil))

	// 2. AuthorityProjectionDigest: authenticates exact mapping semantics (admission class, capabilities, domains, roles, etc.)
	// and incorporates the catalog mapping revision.
	projHasher := sha256.New()
	projHasher.Write([]byte("mapping_revision:" + reg.mappingRevision + "\n"))
	for _, id := range ids {
		r := reg.rules[id]
		projHasher.Write([]byte(canonicalRuleMappingSerialization(r) + "\n"))
	}
	reg.authorityProjectionDigest = "sha256:" + hex.EncodeToString(projHasher.Sum(nil))

	// 3. CatalogDigest: cryptographically authenticates BOTH normative source and authority projection
	catHasher := sha256.New()
	catHasher.Write([]byte(reg.normativeSourceDigest + ":" + reg.authorityProjectionDigest))
	reg.catalogDigest = "sha256:" + hex.EncodeToString(catHasher.Sum(nil))

	reg.frozen = true
	return nil
}

func canonicalRuleMappingSerialization(r Rule) string {
	domains := append([]string(nil), r.Domains...)
	sort.Strings(domains)
	roles := append([]string(nil), r.Roles...)
	sort.Strings(roles)
	risks := append([]string(nil), r.RiskTags...)
	sort.Strings(risks)
	actions := append([]string(nil), r.Actions...)
	sort.Strings(actions)
	paths := append([]string(nil), r.PathPatterns...)
	sort.Strings(paths)
	deps := append([]string(nil), r.DependsOn...)
	sort.Strings(deps)

	return fmt.Sprintf("id=%s|kind=%s|class=%s|cap=%s|domains=%s|roles=%s|risks=%s|actions=%s|paths=%s|deps=%s|rev=%s",
		r.ID,
		r.SourceKind,
		r.AdmissionClass,
		r.Capability,
		strings.Join(domains, ","),
		strings.Join(roles, ","),
		strings.Join(risks, ","),
		strings.Join(actions, ","),
		strings.Join(paths, ","),
		strings.Join(deps, ","),
		r.Revision,
	)
}

// IsFrozen reports whether the registry has been frozen.
func (reg *RuleRegistry) IsFrozen() bool {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.frozen
}
