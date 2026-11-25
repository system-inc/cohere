package react

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// No decoder. Upstream's `meta.schema` is `[]`, confirmed against the installed build.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoRedundantShouldComponentUpdate})
}
