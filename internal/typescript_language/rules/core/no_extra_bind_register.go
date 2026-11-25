package core

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// No `Decode`, because upstream's `meta.schema` is the empty array: this rule has no option surface.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoExtraBind})
}
