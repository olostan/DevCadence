package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ok.go", "package x\n\nfunc F() {}\n")
	var out, errb bytes.Buffer
	if rc := run([]string{dir}, &out, &errb); rc != 0 || out.Len() != 0 {
		t.Fatalf("formatted tree: rc=%d out=%q", rc, out.String())
	}
	write(t, dir, "bad.go", "package x\nfunc  F( ) {\nreturn}\n")
	out.Reset()
	if rc := run([]string{dir}, &out, &errb); rc != 1 || !bytes.Contains(out.Bytes(), []byte("bad.go")) {
		t.Fatalf("unformatted tree: rc=%d out=%q", rc, out.String())
	}
	write(t, dir, "bad.go", "package x\nfunc (\n")
	out.Reset()
	if rc := run([]string{dir}, &out, &errb); rc != 1 {
		t.Fatalf("unparsable file: rc=%d", rc)
	}
	if rc := run(nil, &out, &errb); rc != 2 {
		t.Fatalf("no args: rc=%d", rc)
	}
	if rc := run([]string{filepath.Join(dir, "missing")}, &out, &errb); rc != 2 {
		t.Fatalf("missing path: rc=%d", rc)
	}
}
