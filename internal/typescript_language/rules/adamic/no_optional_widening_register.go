package adamic

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// init registers adamic/no-optional-widening. No decoder: the rule takes no options.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoOptionalWidening})
}
