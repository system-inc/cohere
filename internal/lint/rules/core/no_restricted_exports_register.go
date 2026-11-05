package core

import "github.com/system-inc/cohere/internal/lint/rule"

func init() {
	rule.Register(rule.Registration[NoRestrictedExportsOptions]{
		Rule:   NoRestrictedExports,
		Decode: DecodeNoRestrictedExportsOptions,
	})
}
