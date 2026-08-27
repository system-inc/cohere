package react

import "github.com/system-inc/verify/internal/rule"

// init registers this package's `no-unused-state` rule.
func init() {
	rule.Register(rule.Registration{Rule: NoUnusedState})
}
