package receipts

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/errs"
)

var darwinModePattern = regexp.MustCompile(`^[-dl][-rwxsStT]{9}[@+]?$`)

// parseDarwinLsACL parses /bin/ls -lde output for Darwin ACL presence.
// It fails closed on malformed output, ACL indicators (+), or subsequent ACE lines.
func parseDarwinLsACL(out []byte) (aclAbsent bool, err error) {
	if len(out) == 0 {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: empty output")
	}
	if !utf8.Valid(out) {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: invalid utf-8")
	}
	if bytes.ContainsRune(out, '\r') {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: contains carriage return")
	}

	outStr := string(out)
	if !strings.HasSuffix(outStr, "\n") {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: missing trailing newline")
	}
	trimmed := strings.TrimSuffix(outStr, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: empty lines")
	}

	fields := strings.Fields(lines[0])
	if len(fields) == 0 {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: empty mode line")
	}
	modeStr := fields[0]
	if !darwinModePattern.MatchString(modeStr) {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-probe-unparseable: invalid mode field %q", modeStr)
	}

	if strings.HasSuffix(modeStr, "+") {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-present: mode line indicates ACL (+)")
	}

	if len(lines) > 1 {
		return false, errs.New(errs.CategoryPolicyDenied, "acl-present: ACE lines present (%d lines total)", len(lines))
	}

	if strings.HasSuffix(modeStr, "@") {
		return true, nil
	}

	return true, nil
}
