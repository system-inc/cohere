package react

import "github.com/system-inc/verify/internal/rule"

// init registers this package's `no-access-state-in-setstate` rule.
func init() {
	rule.Register(rule.Registration{Rule: NoAccessStateInSetstate})
}
