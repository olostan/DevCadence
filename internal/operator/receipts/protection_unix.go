//go:build linux || darwin

package receipts

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

// CheckPathProtection verifies that targetPath and all its ancestor components
// satisfy the POSIX protection requirements for operator receipt custody.
func CheckPathProtection(targetPath string, opts ProtectionOptions) error {
	if getEUID() == 0 {
		return errs.New(errs.CategoryPolicyDenied, "refusing protected path check as root")
	}

	if !filepath.IsAbs(targetPath) {
		return errs.New(errs.CategoryInvalidArgument, "path must be absolute: %q", targetPath)
	}

	clean := filepath.Clean(targetPath)
	if targetPath != clean {
		return errs.New(errs.CategoryInvalidArgument, "path must be clean: %q != %q", targetPath, clean)
	}

	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "cannot resolve path %q", clean)
	}
	if canonical != clean {
		return errs.New(errs.CategoryPolicyDenied, "symlinks not allowed in path: %s != %s", clean, canonical)
	}

	trustedUIDs := opts.TrustedOwnerUIDs
	if len(trustedUIDs) == 0 {
		trustedUIDs = []uint32{0, opts.OperatorUID}
	}
	isTrusted := func(uid uint32) bool {
		for _, u := range trustedUIDs {
			if u == uid {
				return true
			}
		}
		return false
	}

	root := "/"
	if opts.RootDir != "" {
		root = filepath.Clean(opts.RootDir)
	}
	if !strings.HasPrefix(clean, root) {
		return errs.New(errs.CategoryInvalidArgument, "target %q is not under root %q", clean, root)
	}

	rel, err := filepath.Rel(root, clean)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to compute relative path from root %q to %q", root, clean)
	}

	components := []string{root}
	if rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		curr := root
		for _, part := range parts {
			if part == "" || part == "." {
				continue
			}
			curr = filepath.Join(curr, part)
			components = append(components, curr)
		}
	}

	for _, comp := range components {
		info, err := os.Lstat(comp)
		if err != nil {
			return errs.Wrap(errs.CategoryNotFound, err, "cannot stat component %s", comp)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errs.New(errs.CategoryPolicyDenied, "component %s is a symlink", comp)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return errs.New(errs.CategoryPolicyDenied, "component %s is group- or world-writable (mode %04o)", comp, info.Mode().Perm())
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errs.New(errs.CategoryPolicyDenied, "cannot determine ownership of component %s", comp)
		}
		if !isTrusted(stat.Uid) {
			return errs.New(errs.CategoryPolicyDenied, "component %s owned by untrusted UID %d", comp, stat.Uid)
		}
		if stat.Uid != 0 && stat.Uid != opts.OperatorUID {
			return errs.New(errs.CategoryPolicyDenied, "component %s owned by UID %d (not root or OperatorUID)", comp, stat.Uid)
		}

		if opts.CheckACL != nil {
			if err := opts.CheckACL(comp); err != nil {
				return err
			}
		} else {
			if err := defaultCheckACL(comp, opts); err != nil {
				return err
			}
		}
	}

	// The file itself and its immediate containing directory must be owned by OperatorUID.
	targetInfo, err := os.Lstat(clean)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "cannot stat target %s", clean)
	}
	targetStat, ok := targetInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return errs.New(errs.CategoryPolicyDenied, "cannot determine ownership of target %s", clean)
	}
	if targetStat.Uid != opts.OperatorUID {
		return errs.New(errs.CategoryPolicyDenied, "target %s owned by UID %d, must be owned by OperatorUID %d", clean, targetStat.Uid, opts.OperatorUID)
	}

	if !targetInfo.IsDir() && clean != root {
		parent := filepath.Dir(clean)
		parentInfo, err := os.Lstat(parent)
		if err != nil {
			return errs.Wrap(errs.CategoryNotFound, err, "cannot stat parent directory %s", parent)
		}
		parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return errs.New(errs.CategoryPolicyDenied, "cannot determine ownership of parent %s", parent)
		}
		if parentStat.Uid != opts.OperatorUID {
			return errs.New(errs.CategoryPolicyDenied, "parent directory %s owned by UID %d, must be owned by OperatorUID %d", parent, parentStat.Uid, opts.OperatorUID)
		}
	}

	return nil
}
