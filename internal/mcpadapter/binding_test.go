package mcpadapter_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/principal"
)

const goodBinding = `{
  "binding_version": "1.0",
  "project_id": "example",
  "principal_id": "principal-1",
  "allowed_actions": ["project_state", "request_evidence", "accept"],
  "policy_ref": "policy-1",
  "max_evidence_bytes": 4096,
  "max_snippet_lines": 100,
  "source_depth": "symbol"
}`

// protectedHome returns a symlink-free home with a private config directory.
func protectedHome(t *testing.T) (home, configDir string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(root, "home")
	configDir = filepath.Join(home, "config")
	for _, dir := range []string{home, configDir, filepath.Join(home, "state")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return home, configDir
}

func writeBinding(t *testing.T, configDir, content string) string {
	t.Helper()
	path := filepath.Join(configDir, "principal-binding.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseBinding(t *testing.T) {
	caller, err := mcpadapter.ParseBinding([]byte(goodBinding))
	if err != nil {
		t.Fatal(err)
	}
	if caller.ProjectID != "example" || caller.PrincipalID != "principal-1" || caller.PolicyRef != "policy-1" ||
		caller.MaxEvidenceBytes != 4096 || caller.MaxSnippetLines != 100 || caller.SourceDepth != "symbol" ||
		len(caller.AllowedActions) != 3 {
		t.Fatalf("unexpected caller %+v", caller)
	}
	empty := strings.Replace(goodBinding, `["project_state", "request_evidence", "accept"]`, `[]`, 1)
	if c, err := mcpadapter.ParseBinding([]byte(empty)); err != nil || len(c.AllowedActions) != 0 {
		t.Fatalf("empty grants must be allowed: %v", err)
	}
	discovery := strings.Replace(goodBinding, `"accept"`, `"record_requirements", "review_specification"`, 1)
	if _, err := mcpadapter.ParseBinding([]byte(discovery)); err != nil {
		t.Fatalf("a discovery grant is a canonical name: %v", err)
	}
	mutate := func(old, new string) []byte { return []byte(strings.Replace(goodBinding, old, new, 1)) }
	for name, doc := range map[string][]byte{
		"unknown field":      mutate(`"policy_ref"`, `"credential": "x", "policy_ref"`),
		"unknown grant":      mutate(`"accept"`, `"accept", "shell"`),
		"alias grant":        mutate(`"accept"`, `"discovery.write"`),
		"duplicate grant":    mutate(`"accept"`, `"project_state"`),
		"grants missing":     mutate(`"allowed_actions": ["project_state", "request_evidence", "accept"],`, ``),
		"version":            mutate(`"1.0"`, `"2.0"`),
		"no policy":          mutate(`"policy_ref": "policy-1"`, `"policy_ref": ""`),
		"depth":              mutate(`"symbol"`, `"file"`),
		"zero bytes":         mutate(`4096`, `0`),
		"too many bytes":     mutate(`4096`, `8193`),
		"zero lines":         mutate(`100`, `0`),
		"too many lines":     mutate(`100`, `201`),
		"null":               mutate(`"policy-1"`, `null`),
		"blank principal":    mutate(`"principal-1"`, `" "`),
		"not json":           []byte(`nope`),
		"trailing":           []byte(goodBinding + `{}`),
		"request field leak": mutate(`"source_depth"`, `"actor": "x", "source_depth"`),
	} {
		if _, err := mcpadapter.ParseBinding(doc); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoadBindingProtections(t *testing.T) {
	t.Run("accepts a protected binding", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		b, err := mcpadapter.LoadBinding(path)
		if err != nil || b.Digest == "" || b.Caller.ProjectID != "example" {
			t.Fatalf("%v %+v", err, b)
		}
	})
	denied := func(t *testing.T, path string) {
		t.Helper()
		_, err := mcpadapter.LoadBinding(path)
		if err == nil {
			t.Fatal("an unprotected binding was loaded")
		}
		if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied && cat != errs.CategoryNotFound {
			t.Fatalf("category %s: %v", cat, err)
		}
	}
	t.Run("relative path", func(t *testing.T) { denied(t, "principal-binding.json") })
	t.Run("missing file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		denied(t, filepath.Join(cfg, "absent.json"))
	})
	t.Run("group readable file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		_ = os.Chmod(path, 0o640)
		denied(t, path)
	})
	t.Run("world writable file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		_ = os.Chmod(path, 0o666)
		denied(t, path)
	})
	t.Run("executable file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		_ = os.Chmod(path, 0o700)
		denied(t, path)
	})
	t.Run("symlinked file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		real := writeBinding(t, cfg, goodBinding)
		link := filepath.Join(cfg, "link.json")
		if err := os.Symlink(real, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		denied(t, link)
	})
	t.Run("symlinked parent", func(t *testing.T) {
		home, cfg := protectedHome(t)
		writeBinding(t, cfg, goodBinding)
		link := filepath.Join(home, "cfglink")
		if err := os.Symlink(cfg, link); err != nil {
			t.Skip("symlinks unavailable")
		}
		denied(t, filepath.Join(link, "principal-binding.json"))
	})
	t.Run("readable parent directory", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		_ = os.Chmod(cfg, 0o755)
		denied(t, path)
	})
	t.Run("group writable ancestor", func(t *testing.T) {
		home, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		_ = os.Chmod(home, 0o770)
		denied(t, path)
	})
	t.Run("directory instead of file", func(t *testing.T) {
		_, cfg := protectedHome(t)
		dir := filepath.Join(cfg, "dir.json")
		_ = os.Mkdir(dir, 0o700)
		denied(t, dir)
	})
	t.Run("other owner", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("needs root to change ownership")
		}
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, goodBinding)
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Skip("chown unavailable")
		}
		denied(t, path)
	})
	t.Run("invalid content", func(t *testing.T) {
		_, cfg := protectedHome(t)
		path := writeBinding(t, cfg, `{"binding_version":"1.0"}`)
		if _, err := mcpadapter.LoadBinding(path); err == nil {
			t.Fatal("an incomplete binding was loaded")
		}
	})
}

// A17: the policy is pinned for the process lifetime.
func TestA17_BindingDriftNeverGrantsNewAuthority(t *testing.T) {
	_, cfg := protectedHome(t)
	path := writeBinding(t, cfg, goodBinding)
	b, err := mcpadapter.LoadBinding(path)
	if err != nil {
		t.Fatal(err)
	}
	policy := mcpadapter.NewBindingPolicy(b)
	ctx := context.Background()
	meta := principal.CallMeta{SchemaVersion: "1.0", ProjectID: "example", CorrelationID: "c"}
	if err := policy.Check(ctx, b.Caller, meta, "project_state"); err != nil {
		t.Fatalf("a bound, granted call was denied: %v", err)
	}
	if err := policy.Check(ctx, b.Caller, meta, "delegate"); err == nil {
		t.Fatal("an ungranted tool was allowed")
	}
	other := meta
	other.ProjectID = "elsewhere"
	if err := policy.Check(ctx, b.Caller, other, "project_state"); err == nil {
		t.Fatal("another project was allowed")
	}
	widened := b.Caller
	widened.AllowedActions = append(append([]string{}, widened.AllowedActions...), "delegate")
	if err := policy.Check(ctx, widened, meta, "delegate"); err == nil {
		t.Fatal("a caller with a widened grant set was allowed")
	}
	renamed := b.Caller
	renamed.PrincipalID = "someone-else"
	if err := policy.Check(ctx, renamed, meta, "project_state"); err == nil {
		t.Fatal("a different principal was allowed")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := policy.Check(cancelled, b.Caller, meta, "project_state"); err == nil {
		t.Fatal("a cancelled context was allowed")
	}

	// The file is edited to grant more: the bound process gains nothing and
	// loses even what it had until an operator relaunches it.
	edited := strings.Replace(goodBinding, `"accept"`, `"accept", "delegate"`, 1)
	writeBinding(t, cfg, edited)
	if err := policy.Check(ctx, b.Caller, meta, "delegate"); err == nil {
		t.Fatal("a changed binding file granted new authority")
	}
	if err := policy.Check(ctx, b.Caller, meta, "project_state"); err == nil {
		t.Fatal("a drifted binding still authorised an action")
	}
	// A deleted file is drift too.
	_ = os.Remove(path)
	if err := policy.Check(ctx, b.Caller, meta, "project_state"); err == nil {
		t.Fatal("a missing binding file authorised an action")
	}
	// Restoring the exact original bytes restores the pinned identity.
	writeBinding(t, cfg, goodBinding)
	if err := policy.Check(ctx, b.Caller, meta, "project_state"); err != nil {
		t.Fatalf("the original binding was refused: %v", err)
	}
}
