//go:build darwin

package receipts

import (
	"context"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
)

// CheckACL executes /bin/ls -lde -- <path> and parses output with parseDarwinLsACL.
func CheckACL(ctx context.Context, runner process.Runner, path string) error {
	for _, c := range path {
		if c < 0x20 || c == 0x7f {
			return errs.New(errs.CategoryPolicyDenied, "path contains control character: %q", path)
		}
	}

	spec := process.Spec{
		Executable:     "/bin/ls",
		Args:           []string{"-lde", "--", path},
		Dir:            "/",
		Env:            []string{"LC_ALL=C", "LANG=C"},
		Timeout:        5 * time.Second,
		MaxStdoutBytes: 4096,
	}

	res, err := runner.Run(ctx, spec)
	if err != nil {
		return errs.Wrap(errs.CategoryPolicyDenied, err, "failed to execute /bin/ls for ACL check")
	}
	if !res.Success() {
		return errs.New(errs.CategoryPolicyDenied, "/bin/ls failed with status %s exit code %d: %s", res.Status, res.ExitCode, string(res.Stderr))
	}

	aclAbsent, err := parseDarwinLsACL(res.Stdout)
	if err != nil {
		return err
	}
	if !aclAbsent {
		return errs.New(errs.CategoryPolicyDenied, "ACL present on path: %s", path)
	}
	return nil
}

func defaultCheckACL(path string, opts ProtectionOptions) error {
	runner := opts.Runner
	if runner == nil {
		runner = process.NewRunner()
	}
	return CheckACL(context.Background(), *runner, path)
}
