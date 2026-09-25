package credentials

import (
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ValidateProcessSpecNoSecrets enforces the process boundary rule from ADR-0014 §6:
// Raw secrets MUST NOT appear in process.Spec.Args or process.Spec.Env.
func ValidateProcessSpecNoSecrets(spec process.Spec) error {
	// Check Args
	for _, arg := range spec.Args {
		if protocol.LooksLikeSecret(arg) {
			return errs.New(errs.CategoryInvalidArgument,
				"process.Spec: argument looks like a secret value; raw secrets must never be passed in argv")
		}
	}

	// Check Env
	for _, entry := range spec.Env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			val := parts[1]
			if protocol.LooksLikeSecret(val) {
				return errs.New(errs.CategoryInvalidArgument,
					"process.Spec: environment variable %s contains a secret-looking value", parts[0])
			}
		} else if protocol.LooksLikeSecret(entry) {
			return errs.New(errs.CategoryInvalidArgument,
				"process.Spec: environment entry looks like a secret value")
		}
	}

	return nil
}
