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
// unregistered or registered with another decoder fails by name.
func TestCompilerRulesAcceptOnlyTheEmptyObject(test *testing.T) {
	test.Parallel()

	decoders := map[string]func([]byte) (any, error){}
	for _, registration := range rule.Registered() {
		decoders[registration.Rule.Name] = registration.Decode
	}

	for _, name := range compilerRuleNames {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			decode := decoders[name]
			if decode == nil {
				test.Fatalf("%s registers no decoder, so the config refuses the {} ESLint writes", name)
			}
			for _, raw := range []string{``, `{}`, ` { } `} {
				if _, decodeError := decode([]byte(raw)); decodeError != nil {
					test.Errorf("%q was refused: %v", raw, decodeError)
				}
			}
			_, decodeError := decode([]byte(`{"environment": {"validateRefAccessDuringRender": false}}`))
			if decodeError == nil || !strings.Contains(decodeError.Error(), "environment") ||
				!strings.Contains(decodeError.Error(), "React Compiler configuration") {
				test.Errorf("compiler configuration was not refused with its reason: %v", decodeError)
			}
			for _, raw := range []string{`null`, `[]`, `"strict"`, `true`} {
				if _, decodeError := decode([]byte(raw)); decodeError == nil {
					test.Errorf("%s decoded; upstream's schema takes an object", raw)
				}
			}
		})
	}
}
