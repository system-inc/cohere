package adamic

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// init registers adamic/single-spread. No decoder: a later spread is a hole whatever the project prefers.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: SingleSpread})
}
