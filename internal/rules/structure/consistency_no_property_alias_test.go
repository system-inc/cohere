package structure

import (
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

const propertyAliasFile = "/repository/source/Thing.ts"

// The first two cases are the shapes of the only two live violations in the ahra tree, both of
// which carry an `eslint-disable-next-line` for this rule. They are the closest thing to an answer
// key this rule has: it is ours, so there is no upstream corpus, and the tree reports zero findings
// precisely because those two sites are suppressed.

func TestConsistencyNoPropertyAliasFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"the real Run.ts shape", "export function run(options: OptionsInterface) {\n    const onOutputLine = options.onOutputLine;\n    return onOutputLine;\n}\n"},
		{"the real TrackedPromise shape", "export function run(tracked: TrackedInterface) {\n    const promise = tracked.promise;\n    return promise;\n}\n"},
		// The Run.ts shape, where the reach is through a cast. The cast is the reason the alias
		// exists (it narrows once for four later reads), which is why the site carries a disable
		// comment rather than being rewritten, and it must still be seen as an alias.
		{"a reach through a cast", "export function run(options: OptionsInterface) {\n    const onOutputLine = (options as ProgramOptionsInterface).onOutputLine;\n    return onOutputLine;\n}\n"},
		{"a nested reach", "export function run(state: StateInterface) {\n    const value = state.inner.value;\n    return value;\n}\n"},
		{"a long reach with no call anywhere in it", "export function run(a: AInterface) {\n    const value = a.one.two.three.value;\n    return value;\n}\n"},
		{"inside an arrow function", "export const Run = (options: OptionsInterface) => {\n    const timeout = options.timeout;\n    return timeout;\n};\n"},
		{"inside a method", "export class Thing {\n    run(options: OptionsInterface) {\n        const limit = options.limit;\n        return limit;\n    }\n}\n"},
		{"with a let binding", "export function run(options: OptionsInterface) {\n    let cursor = options.cursor;\n    return cursor;\n}\n"},
		// The hatch is for hooks, and only hooks. This rule reports when it fails to find the
		// exemption, so its polarity is inverted from most: a hatch that is too WIDE loses
		// findings quietly, and a hatch that is too NARROW manufactures accusations against
		// correct files. 117 of the 231 alias-shaped declarations on the ahra tree are exempted by
		// this hatch alone, so both directions get a case.
		{"an array argument to something that is not a hook", "export function run(options: OptionsInterface) {\n    const secret = options.secret;\n    notAHook(function() {\n        return 1;\n    }, [secret]);\n    return secret;\n}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t, rule_testing.Run(t, ConsistencyNoPropertyAlias, propertyAliasFile, testCase.sourceText),
				"noPropertyAlias")
		})
	}
}

// The clean half is the specification. Every exemption here is a judgment rather than a narrowing,
// and each is the kind that reads as a bug until its reason is stated.

func TestConsistencyNoPropertyAliasStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"the names differ", "export function run(options: OptionsInterface) {\n    const value = options.other;\n    return value;\n}\n"},
		{"a call in the chain", "export function run() {\n    const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;\n    return timeZone;\n}\n"},
		{"a call at the head of the chain", "export function run(make: MakeInterface) {\n    const value = make().value;\n    return value;\n}\n"},
		// The call sits two links up rather than beside the property, which is what makes the
		// descent load-bearing. A mutation sweep produced this case: stopping the walk after one
		// link left every other fixture green, because each of them had its call at the head or
		// one step in. The chain still computes, so the local is still caching rather than
		// aliasing, and the exemption has to survive the extra depth.
		{"a call deeper in the chain", "export function run(a: AInterface) {\n    const value = a.make().inner.value;\n    return value;\n}\n"},
		// The call sits BELOW an element access, which is the only shape that exercises the
		// element-access arm of the chain walk. It is here because reasoning said the arm was dead
		// and a probe said otherwise: `a[k].make()` finds its call before ever descending, but
		// `make()[k]` does not, so deleting the arm turns a computed-caching case into a reported
		// alias. Argument lost to measurement.
		{"a call below an element access", "export function run(make: MakeInterface, key: string) {\n    const value = make()[key].value;\n    return value;\n}\n"},
		// Parentheses around a computing chain. The exemption has to see through them, because a
		// reader who wrapped the call in parentheses did not stop it computing.
		{"a parenthesized call", "export function run(make: MakeInterface) {\n    const value = (make()).value;\n    return value;\n}\n"},
		{"computed access", "export function run(record: RecordInterface, name: string) {\n    const key = record[name];\n    return key;\n}\n"},
		// Optional chains, which are 78 of the 231 alias-shaped declarations on the ahra tree and
		// the single largest exempted group. ESTree wraps `a?.b` in a ChainExpression, so the
		// original's `init.type !== 'MemberExpression'` test rejects it before doing anything;
		// typescript-go has no wrapper, so the exemption has to be written here or the rule
		// accuses 78 correct files.
		{"an optional access", "export function run(options: OptionsInterface) {\n    const value = options?.value;\n    return value;\n}\n"},
		// The token sits two links in rather than at the outermost access. An exemption that
		// checked only the outer link left exactly this shape reporting on the real tree.
		{"an optional link deeper in the chain", "export function run(account: AccountInterface) {\n    const displayName = account.data?.profile.displayName;\n    return displayName;\n}\n"},
		{"an optional element access", "export function run(o: OInterface, index: string) {\n    const countryCode = o.locales[index]?.countryCode;\n    return countryCode;\n}\n"},
		{"at module scope", "export const Foo = Bar.Foo;\n"},
		{"no initializer", "export function run() {\n    let value;\n    return value;\n}\n"},
		{"a destructure rather than a reach", "export function run(options: OptionsInterface) {\n    const { timeout } = options;\n    return timeout;\n}\n"},
		{"an initializer that is not a reach", "export function run() {\n    const value = 1;\n    return value;\n}\n"},
		{"read in a useEffect dependency array", "export function run(options: OptionsInterface) {\n    const timeout = options.timeout;\n    React.useEffect(function() {\n        report(timeout);\n    }, [timeout]);\n}\n"},
		{"read in a bare hook dependency array", "export function run(options: OptionsInterface) {\n    const timeout = options.timeout;\n    useMemo(function() {\n        return timeout;\n    }, [timeout]);\n}\n"},
		{"read inside a longer reach in a dependency array", "export function run(options: OptionsInterface) {\n    const settings = options.settings;\n    useMemo(function() {\n        return 1;\n    }, [settings.value]);\n}\n"},
		{"read as a deep reach in a dependency array", "export function run(options: OptionsInterface) {\n    const secret = options.secret;\n    useMemo(function() {\n        return 1;\n    }, [secret.id.value]);\n}\n"},
		{"read in a dependency array inside a nested callback", "export function run(options: OptionsInterface) {\n    const secret = options.secret;\n    wrap(function() {\n        useMemo(function() {\n            return 1;\n        }, [secret]);\n    });\n}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.Run(t, ConsistencyNoPropertyAlias, propertyAliasFile, testCase.sourceText))
		})
	}
}
