package adamic

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoCrossEnumNarrowing})
}
