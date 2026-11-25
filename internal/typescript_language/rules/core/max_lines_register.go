package core

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[MaxLinesOptions]{
		Rule:   MaxLines,
		Decode: DecodeMaxLinesOptions,
	})
}
