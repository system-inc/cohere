package react

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// init registers this package's `no-unused-state` rule.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoUnusedState})
}
