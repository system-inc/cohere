package core

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(rule.Registration[PreferRegexLiteralsOptions]{
		Rule:   PreferRegexLiterals,
		Decode: rule.DecodeOptionsInto[PreferRegexLiteralsOptions](),
	})
}
