// Command fmtcheck exits non-zero when any Go file under the given paths is not
// gofmt-formatted (or does not parse). It exists because `gofmt -l` exits 0 even
// when it lists unformatted files, and `make fmt-check` only inspects files that
// are already committed relative to origin/main, which a not-yet-committed
// candidate worktree is not. Used by .devcadence/validation.yaml.
//
// Usage: fmtcheck PATH...   (files, or directories walked recursively)
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run returns 0 when every Go file under paths is formatted, 1 when at least one
// is not, and 2 on usage or I/O errors.
func run(paths []string, stdout, stderr io.Writer) int {
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: fmtcheck PATH...")
		return 2
	}
	bad, failed := 0, false
	for _, root := range paths {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out, ferr := format.Source(src)
			if ferr != nil {
				fmt.Fprintf(stdout, "%s: %v\n", p, ferr)
				bad++
				return nil
			}
			if !bytes.Equal(src, out) {
				fmt.Fprintf(stdout, "%s: not gofmt-formatted\n", p)
				bad++
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(stderr, "fmtcheck: %v\n", err)
			failed = true
		}
	}
	switch {
	case failed:
		return 2
	case bad > 0:
		return 1
	}
	return 0
}
