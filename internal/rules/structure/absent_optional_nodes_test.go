package structure

import (
	"fmt"
	"testing"

	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// Every rule in this package is run over shapes where a node it reaches for is legitimately absent.
//
// This exists because react-component-no-display-name shipped a crash that review did not catch and
// only a fixture did. `ast.SkipParentheses` dereferences its argument, so a nil check placed after
// the call rather than before it panics, and the shapes that produce a nil are ordinary: a
// `declare const` with no initializer, a `let` with no initializer, a call with no arguments, an
// object shorthand property with no explicit value.
//
// A crash is worse than a wrong finding. A wrong finding is visible and arguable; a panic takes
// down the walk for the whole file, so every other rule's verdict on that file is lost too, and on
// a large tree the file that crashed is one line in a log nobody reads.
//
// Kept as a standing guard rather than deleted after the fix, because the pattern recurs: nearly
// every rule in this family reaches for an initializer, an argument, or a type annotation that does
// not have to exist. Add new rules to the list.
func TestNoRuleCrashesOnAbsentOptionalNodes(t *testing.T) {
	sources := []string{
		"declare const memo: unknown;\nexport const value = 1;\n",
		"import { networkService } from './NetworkService.ts';\nlet useThingRequest;\nexport const value = useThingRequest;\n",
		"import { networkService } from './NetworkService.ts';\ndeclare const Document: unknown;\n" +
			"export function useThingRequest(o?: unknown) { return networkService.useGraphQlQuery(Document, { shorthand }, o); }\n" +
			"declare const shorthand: unknown;\n",
		"import { networkService } from './NetworkService.ts';\nexport function useThingRequest() { return networkService.useGraphQlQuery(); }\n",
		"declare const Field: { displayName: string };\nlet other;\nexport const value = other;\n",
		"export function Page() { return null; }\n",
	}

	rules := []rule.Rule{
		NetworkNoStringLiteralQuery,
		NetworkRequireHookRequestSuffix,
		NetworkRequireHookOptionsParameter,
		NetworkRequireHookVariablesType,
		ReactNoAnchorElement,
		ReactNoHorizontalRuleElement,
		ReactComponentNoForwardRef,
		ReactComponentNoDisplayName,
	}

	for _, currentRule := range rules {
		for index, source := range sources {
			t.Run(fmt.Sprintf("%s/shape-%d", currentRule.Name, index), func(t *testing.T) {
				// A panic fails the test. The assertion is only that this returns at all, so
				// findings are deliberately not checked: what each rule concludes about these
				// shapes belongs in its own pair, and asserting it here would make this guard
				// fail for reasons that are not crashes.
				ruletest.Run(t, currentRule, "/repository/source/api/ThingRequest.ts", source)
			})
		}
	}
}
