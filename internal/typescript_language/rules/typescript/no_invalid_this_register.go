package typescript

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[noInvalidThisSettings]{
		Rule:   NoInvalidThis,
		Decode: DecodeNoInvalidThisOptions,
	})
}
