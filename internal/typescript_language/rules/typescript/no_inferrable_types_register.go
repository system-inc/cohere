package typescript

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[NoInferrableTypesOptions]{
		Rule:   NoInferrableTypes,
		Decode: DecodeNoInferrableTypesOptions,
	})
}
