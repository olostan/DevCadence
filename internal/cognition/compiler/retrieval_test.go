package compiler_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestOptionalRetrievalEngine_LookupAndSearch(t *testing.T) {
	engine := compiler.NewOptionalRetrievalEngine()

	// Register items
	item1 := compiler.OptionalItem{
		ID:           "ADR-0019",
		Kind:         "adr",
		Title:        "Non-conversational cognition and adaptive review",
		Content:      "Details on silent multi-dimensional metering and context controls.",
		Symbols:      []string{"ContextControl", "PrefixCache"},
		Paths:        []string{"docs/adr/0019-non-conversational-cognition-and-adaptive-review.md"},
		Keywords:     []string{"metering", "budget", "session"},
		Dependencies: []string{"ADR-0020"},
	}
	item2 := compiler.OptionalItem{
		ID:           "ADR-0020",
		Kind:         "adr",
		Title:        "Cognitive Invocation Compiler and Review Ledger",
		Content:      "Details on deterministic rule admission, reverse coverage, and prompt projection.",
		Symbols:      []string{"CognitiveCompiler", "RuleRegistry"},
		Paths:        []string{"docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md"},
		Keywords:     []string{"compiler", "admission", "reverse-coverage"},
		Dependencies: []string{"DOC-INVARIANTS"},
	}
	item3 := compiler.OptionalItem{
		ID:       "DOC-INVARIANTS",
		Kind:     "doc",
		Title:    "Architectural Invariants",
		Content:  "Normative invariants catalog.",
		Keywords: []string{"invariants", "rules"},
	}

	for _, item := range []compiler.OptionalItem{item1, item2, item3} {
		if err := engine.RegisterItem(item); err != nil {
			t.Fatalf("failed to register %s: %v", item.ID, err)
		}
	}

	// 1. Lookup by ID
	it, ok := engine.LookupByID("ADR-0019")
	if !ok || it.Title != item1.Title {
		t.Fatalf("lookup by ID failed: got %v, ok %v", it, ok)
	}

	// 2. Lookup by symbol
	bySymbol := engine.LookupBySymbol("ContextControl")
	if len(bySymbol) != 1 || bySymbol[0].ID != "ADR-0019" {
		t.Fatalf("lookup by symbol failed: %v", bySymbol)
	}

	// 3. Lookup by path
	byPath := engine.LookupByPath("docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md")
	if len(byPath) != 1 || byPath[0].ID != "ADR-0020" {
		t.Fatalf("lookup by path failed: %v", byPath)
	}

	// 4. Lexical Search
	results := engine.LexicalSearch("deterministic compiler", 5)
	if len(results) == 0 {
		t.Fatal("expected lexical search results, got 0")
	}
	if results[0].ID != "ADR-0020" {
		t.Fatalf("expected top search result to be ADR-0020, got %s", results[0].ID)
	}

	// 5. Graph Traversal
	// ADR-0019 -> ADR-0020 -> DOC-INVARIANTS
	traversed := engine.TraverseGraph("ADR-0019", 3)
	if len(traversed) != 2 {
		t.Fatalf("expected 2 traversed dependencies, got %d: %v", len(traversed), traversed)
	}
	found20 := false
	foundDoc := false
	for _, tr := range traversed {
		if tr.ID == "ADR-0020" {
			found20 = true
		}
		if tr.ID == "DOC-INVARIANTS" {
			foundDoc = true
		}
	}
	if !found20 || !foundDoc {
		t.Fatalf("graph traversal did not find all dependencies: %v", traversed)
	}
}

func TestEnsureMandatoryInviolability(t *testing.T) {
	mandatory := []protocol.MandatoryClauseRef{
		{
			ClauseID:      "DCI-018",
			SourceDoc:     "INVARIANTS.md",
			Revision:      "v1",
			ContentDigest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	// Candidate optional items where one attempts to reuse or override mandatory clause ID
	candidates := []compiler.OptionalItem{
		{
			ID:      "DCI-018", // Collides with mandatory clause!
			Title:   "Attacker attempted override",
			Content: "Weakened rule",
		},
		{
			ID:      "ADR-0019",
			Title:   "Valid optional background",
			Content: "Safe background material",
		},
	}

	safe := compiler.EnsureMandatoryInviolability(mandatory, candidates)
	if len(safe) != 1 || safe[0].ID != "ADR-0019" {
		t.Fatalf("EnsureMandatoryInviolability failed: got %v, want only ADR-0019", safe)
	}
}
