package react

import "github.com/system-inc/cohere/internal/typescript_language/rule"

func init() {
	rule.Register(
		rule.Registration[CompilerRuleOptions]{Rule: NoDerivingStateInEffects, Decode: DecodeCompilerRuleOptions},
	)
}
