package core

import "github.com/system-inc/verify/internal/rule"

// init registers radix.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// No decoder, deliberately. Upstream's schema still accepts "always" or "as-needed" and its rule
// body never reads context.options; its own corpus proves the option is vestigial by shipping the
// same source as a passing case under both values. Registering no decoder makes an option written in
// the config a loud error rather than a silent no-op.
func init() {
	rule.Register(rule.Registration{Rule: Radix})
}
