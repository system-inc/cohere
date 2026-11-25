package core

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[MaxNestedCallbacksOptions]{
		Rule:   MaxNestedCallbacks,
		Decode: DecodeMaxNestedCallbacksOptions,
	})
}
