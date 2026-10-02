package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// OptionalItem represents an optional reference or background knowledge object
// (ADR, design rationale, architectural note, symbol doc) retrieved for context discovery.
type OptionalItem struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"` // "rationale", "adr", "reference", "doc", "schema"
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	ContentDigest string   `json:"content_digest"`
	Symbols       []string `json:"symbols,omitempty"`
	Paths         []string `json:"paths,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"` // explicit graph edges: item -> item or item -> rule
}

// Validate checks OptionalItem basic integrity.
func (item OptionalItem) Validate() error {
	const kind = "OptionalItem"
	if item.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: id cannot be empty", kind)
	}
	if item.Content == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: content cannot be empty", kind)
	}
	return nil
}

// OptionalRetrievalEngine provides exact, lexical, and graph-traversal discovery
// for non-mandatory background knowledge (ADR-0020 §3).
//
// Inviolable Invariant (DCI-132, ADR-0020 §2, §3):
// Optional retrieval serves purely as an auxiliary recall and discovery mechanism.
// It MUST NEVER evict, override, or drop mandatory clauses.
type OptionalRetrievalEngine struct {
	mu    sync.RWMutex
	items map[string]OptionalItem
}

// NewOptionalRetrievalEngine creates a new OptionalRetrievalEngine.
func NewOptionalRetrievalEngine() *OptionalRetrievalEngine {
	return &OptionalRetrievalEngine{
		items: make(map[string]OptionalItem),
	}
}

// RegisterItem registers an optional knowledge item into the engine.
func (e *OptionalRetrievalEngine) RegisterItem(item OptionalItem) error {
	if err := item.Validate(); err != nil {
		return err
	}
	if item.ContentDigest == "" {
		hasher := sha256.New()
		hasher.Write([]byte(item.Content))
		item.ContentDigest = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.items[item.ID] = item
	return nil
}

// LookupByID retrieves an item by its exact identifier.
func (e *OptionalRetrievalEngine) LookupByID(id string) (OptionalItem, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	item, ok := e.items[id]
	return item, ok
}

// LookupBySymbol finds all items associated with a given code symbol or identifier.
func (e *OptionalRetrievalEngine) LookupBySymbol(symbol string) []OptionalItem {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var matched []OptionalItem
	target := strings.ToLower(strings.TrimSpace(symbol))
	if target == "" {
		return nil
	}

	for _, item := range e.items {
		for _, s := range item.Symbols {
			if strings.ToLower(s) == target {
				matched = append(matched, item)
				break
			}
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ID < matched[j].ID
	})
	return matched
}

// LookupByPath finds items associated with a specific file path.
func (e *OptionalRetrievalEngine) LookupByPath(filePath string) []OptionalItem {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var matched []OptionalItem
	target := strings.ToLower(strings.TrimSpace(filePath))
	if target == "" {
		return nil
	}

	for _, item := range e.items {
		for _, p := range item.Paths {
			if strings.ToLower(p) == target || strings.Contains(strings.ToLower(p), target) {
				matched = append(matched, item)
				break
			}
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ID < matched[j].ID
	})
	return matched
}

// LexicalSearch performs scored term-matching across titles, contents, and keywords.
func (e *OptionalRetrievalEngine) LexicalSearch(query string, limit int) []OptionalItem {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queryTerms := strings.Fields(strings.ToLower(query))
	if len(queryTerms) == 0 {
		return nil
	}

	type scoredItem struct {
		item  OptionalItem
		score int
	}

	var scored []scoredItem
	for _, item := range e.items {
		titleLower := strings.ToLower(item.Title)
		contentLower := strings.ToLower(item.Content)
		score := 0

		for _, term := range queryTerms {
			if strings.Contains(titleLower, term) {
				score += 5
			}
			for _, kw := range item.Keywords {
				if strings.ToLower(kw) == term {
					score += 3
				}
			}
			for _, sym := range item.Symbols {
				if strings.ToLower(sym) == term {
					score += 4
				}
			}
			if strings.Contains(contentLower, term) {
				score += 1
			}
		}

		if score > 0 {
			scored = append(scored, scoredItem{item: item, score: score})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].item.ID < scored[j].item.ID
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]OptionalItem, len(scored))
	for i, s := range scored {
		result[i] = s.item
	}
	return result
}

// TraverseGraph traverses explicit dependency edges (item -> item) up to maxDepth.
func (e *OptionalRetrievalEngine) TraverseGraph(startID string, maxDepth int) []OptionalItem {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if maxDepth <= 0 {
		maxDepth = 5
	}

	visited := make(map[string]bool)
	var result []OptionalItem
	queue := []string{startID}
	depth := 0

	for len(queue) > 0 && depth < maxDepth {
		levelSize := len(queue)
		for i := 0; i < levelSize; i++ {
			currID := queue[0]
			queue = queue[1:]

			if visited[currID] {
				continue
			}
			visited[currID] = true

			item, ok := e.items[currID]
			if !ok {
				continue
			}
			if currID != startID {
				result = append(result, item)
			}

			for _, depID := range item.Dependencies {
				if !visited[depID] {
					queue = append(queue, depID)
				}
			}
		}
		depth++
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// EnsureMandatoryInviolability enforces the core invariant:
// Optional retrieval MUST NEVER evict, override, or replace mandatory clauses (DCI-132).
// If any optional candidate attempts to suppress or conflict with a mandatory clause, it is rejected.
func EnsureMandatoryInviolability(mandatory []protocol.MandatoryClauseRef, candidates []OptionalItem) []OptionalItem {
	mandatorySet := make(map[string]bool, len(mandatory))
	for _, m := range mandatory {
		mandatorySet[m.ClauseID] = true
	}

	// Filter out any optional candidates that attempt to claim authority over or duplicate mandatory clauses
	var safeCandidates []OptionalItem
	for _, cand := range candidates {
		// An optional item cannot have the same ID as a mandatory clause (mandatory is authoritative)
		if !mandatorySet[cand.ID] {
			safeCandidates = append(safeCandidates, cand)
		}
	}
	return safeCandidates
}
