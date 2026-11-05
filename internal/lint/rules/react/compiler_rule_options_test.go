package react

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// compilerRuleNames are the fifteen React Compiler rules that share upstream's one options schema.
var compilerRuleNames = []string{
	"react-hooks/config",
	"react-hooks/error-boundaries",
	"react-hooks/globals",
	"react-hooks/immutability",
	"react-hooks/incompatible-library",
	"react-hooks/no-deriving-state-in-effects",
	"react-hooks/preserve-manual-memoization",
	"react-hooks/purity",
	"react-hooks/refs",
	"react-hooks/set-state-in-effect",
	"react-hooks/set-state-in-render",
	"react-hooks/static-components",
	"react-hooks/unsupported-syntax",
	"react-hooks/use-memo",
	"react-hooks/void-use-memo",
}

// TestCompilerRulesAcceptOnlyTheEmptyObject pins ruling 2 on #e06zm4b for every one of the fifteen,
// through the decoder each one registers rather than the shared function, so a rule left
// unregistered or registered with another decoder fails by name. An entry's Bind runs that decoder and
// returns its error, which is all this reads.
func TestCompilerRulesAcceptOnlyTheEmptyObject(test *testing.T) {
	test.Parallel()

	binders := map[string]func(raw []byte, base rule.OptionsBase) (rule.Bound, error){}
	for _, entry := range rule.Registered() {
		// The rows below are single option elements, which only a rule registered with Decode is handed,
		// so a rule registered with another arity reads as having no decoder for them.
		if entry.Arity == rule.OptionArityOne {
			binders[entry.Name] = entry.Bind
		}
	}

	for _, name := range compilerRuleNames {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			bind := binders[name]
			if bind == nil {
				test.Fatalf("%s registers no decoder, so the config refuses the {} ESLint writes", name)
			}
			for _, raw := range []string{``, `{}`, ` { } `} {
				if _, bindError := bind([]byte(raw), rule.OptionsBase{}); bindError != nil {
					test.Errorf("%q was refused: %v", raw, bindError)
				}
			}
			_, bindError := bind(
				[]byte(`{"environment": {"validateRefAccessDuringRender": false}}`),
				rule.OptionsBase{},
			)
			if bindError == nil || !strings.Contains(bindError.Error(), "environment") ||
				!strings.Contains(bindError.Error(), "React Compiler configuration") {
				test.Errorf("compiler configuration was not refused with its reason: %v", bindError)
			}
			for _, raw := range []string{`null`, `[]`, `"strict"`, `true`} {
				if _, bindError := bind([]byte(raw), rule.OptionsBase{}); bindError == nil {
					test.Errorf("%s decoded; upstream's schema takes an object", raw)
				}
			}
		})
	}
}
