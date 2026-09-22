package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/events"
)

// TestDocumentedEventTypesAreImplemented detects drift between the event
// vocabulary ENGINEERING_STANDARDS.md §11 documents and the one this build
// registers.
//
// This exists because that drift already happened once: the discovery
// subsystem added eight event types to §11 in parallel with the control-plane
// implementation, and nothing failed until a human noticed. AGENTS.md §10
// asks for documentation-to-code drift detection; for the event vocabulary,
// this is it.
//
// The check is one-directional. §11 is a floor, not a ceiling: the
// implementation may register events the document does not list — it says so
// itself — but it may not omit one the document names.
func TestDocumentedEventTypesAreImplemented(t *testing.T) {
	section := eventModelSection(t)
	documented := bulletedNames(section)
	if len(documented) < 20 {
		t.Fatalf("parsed only %d event names from ENGINEERING_STANDARDS.md §11; "+
			"the section format probably changed and this check is no longer reading it",
			len(documented))
	}
	for _, name := range documented {
		if !events.Registered(events.Type(name)) {
			t.Errorf("ENGINEERING_STANDARDS.md §11 documents event %q, but it is not registered", name)
		}
	}
}

// eventModelSection returns the body of "## 11. Event model".
func eventModelSection(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../ENGINEERING_STANDARDS.md")
	if err != nil {
		t.Fatalf("read ENGINEERING_STANDARDS.md: %v", err)
	}
	text := string(raw)
	start := strings.Index(text, "## 11. Event model")
	if start < 0 {
		t.Fatal("ENGINEERING_STANDARDS.md has no '## 11. Event model' section")
	}
	rest := text[start+len("## 11. Event model"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// eventNamePattern matches a list bullet holding a bare UpperCamelCase event
// name, which is how §11 writes them. Prose bullets and back-ticked mentions
// in the surrounding paragraphs do not match.
var eventNamePattern = regexp.MustCompile(`^- ([A-Z][A-Za-z]+)$`)

func bulletedNames(section string) []string {
	var out []string
	for _, line := range strings.Split(section, "\n") {
		if match := eventNamePattern.FindStringSubmatch(strings.TrimRight(line, "\r")); match != nil {
			out = append(out, match[1])
		}
	}
	return out
}
