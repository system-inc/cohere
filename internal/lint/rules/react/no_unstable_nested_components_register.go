package react

import "github.com/system-inc/cohere/internal/lint/rule"

func init() {
	rule.Register(rule.Registration[NoUnstableNestedComponentsOptions]{
		Rule:   NoUnstableNestedComponents,
		Decode: DecodeNoUnstableNestedComponentsOptions,
	})
}
