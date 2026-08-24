package reactconformance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ShippedRules are the upstream rule names verify has an implementation for.
//
// Keyed by UPSTREAM's rule name rather than verify's, because the join to the corpus goes through
// upstream's `getRuleForCategory`. The two happen to agree on spelling for every rule here, which
// is convenient and is not relied on: the map is explicit so a rename on either side is a visible
// edit rather than a silent unjoin.
//
// `hooks` maps to verify's `rules-of-hooks`, which is the one place the names differ.
var ShippedRules = map[string]string{
	"config":              "react/config",
	"error-boundaries":    "react/error-boundaries",
	"gating":              "react/gating",
	"globals":             "react/globals",
	"hooks":               "react/rules-of-hooks",
	"set-state-in-effect": "react/set-state-in-effect",
	"set-state-in-render": "react/set-state-in-render",
	"static-components":   "react/static-components",
	"unsupported-syntax":  "react/unsupported-syntax",
	"use-memo":            "react/use-memo",
	"void-use-memo":       "react/void-use-memo",
}

// TypeAwareRules are the shipped rules that decide by asking the type checker.
//
// This list is what makes `VerdictUnresolvableTypes` a measurement rather than an excuse: only a
// rule that actually reads types can be excused by a fixture that has none. A syntax-only rule
// producing nothing on an import-less fixture is a failure, and must be scored as one.
//
// Membership was read from the rule sources, not guessed. Each of these calls `ctx.TypeChecker` on
// the path that decides whether to report, and each ships fixtures carrying a local `react.d.ts`
// for exactly this reason (`set_state_in_effect_test.go:57`, `set_state_in_render_test.go:79`).
var TypeAwareRules = map[string]bool{
	"set-state-in-effect": true,
	"set-state-in-render": true,
	"static-components":   true,
}

// reactImportPattern matches an import or require of `react`.
//
// Written to match the module specifier exactly rather than the substring `react`, which would also
// match `react-dom`, `@testing-library/react`, and a local `./react-utils`. That over-match would
// move fixtures out of `VerdictUnresolvableTypes` and into `failed`, inflating the failure count
// with cases that are really corpus artifacts — an error in the direction that looks like rigor.
var reactImportPattern = regexp.MustCompile(`(?m)(?:^|\n)\s*import\b[^\n;]*?['"]react['"]|require\(\s*['"]react['"]\s*\)`)

// hookCallPattern matches a call to something named like a hook.
var hookCallPattern = regexp.MustCompile(`\buse[A-Z]\w*\s*\(`)

// ResolvesReactTypes reports whether a fixture could resolve `@types/react`.
func (f Fixture) ResolvesReactTypes() bool {
	return reactImportPattern.MatchString(f.Source)
}

// CallsHook reports whether a fixture calls anything named like a hook.
func (f Fixture) CallsHook() bool {
	return hookCallPattern.MatchString(f.Source)
}

// StatedDivergence names a fixture verify deliberately does not reproduce, with where the boundary
// is written down.
//
// Entries are the machine-checked ones. A divergence that lives only in a doc comment is not in
// here, because the entry's whole value is that something else fails when the divergence stops
// being true — otherwise this map is a place to park failures with a nice label on them.
type StatedDivergence struct {
	// Fixture is the corpus fixture, by name.
	Fixture string

	// Boundary is the test that holds the divergence in place.
	Boundary string

	// Reason is what verify decided and why.
	Reason string
}

// statedDivergences names the fixtures verify deliberately does not reproduce.
//
// The stated boundaries verify's react rules carry were all measured against oxc's inline corpus or
// against the executable ESLint rule, and every one of them was checked against this corpus while
// building this file. None of them lands on a fixture in the 325:
//
//   - exhaustive-deps at 15 of 17 kinds (`TestExhaustiveDepsScopeIsStated`, eight named cases): its
//     rule is `exhaustive-deps`, an ESLint rule. The corpus's nearest categories are
//     `memo-dependencies` (14 fixtures) and `exhaustive-effect-dependencies` (4), which are the
//     COMPILER's dependency validators and a different mechanism. Neither is a rule verify ships.
//   - set-state-in-effect's ref exemption, pinned as a fixture that fails when fixed: that rule has
//     zero error-named fixtures in this corpus, so the divergence cannot be exercised here.
//   - no-unused-vars at 301 of 303: not a react rule and not in this corpus at all.
//
// The eleven entries below are `globals` fixtures, and they are here because the rule was actually
// RUN against them (`internal/reactconformancescore`) rather than because a doc comment predicted
// them. Every one declines on a construct its doc comment already names as out of scope: eight on
// the nested-function subset that needs an inter-procedural effects pass, three on the compilation
// gate that requires JSX or a hook call in the body.
//
// This is the category the brief was most worried about, and the measurement justifies the worry
// exactly: a raw score would have reported `react/globals` at 0 of 11 and read as a broken rule. It
// is a rule with a written, machine-checked boundary meeting a corpus of minimal repros that sit
// entirely outside it.
//
// What that means for the number is worth stating plainly rather than leaving implicit: 0 passed
// and 11 excluded is NOT the same claim as 11 passed. The rule is unmeasured against this corpus,
// not validated by it. The honest reading is that React's error goldens for `globals` and verify's
// `globals` do not overlap, and closing that gap is a scope decision rather than a bug fix.
var statedDivergences = map[string]StatedDivergence{
	// The nested-function subset. `react/globals` reports only writes lexically inside the
	// compilation root's own body, never inside a nested function, because deciding whether a
	// nested function runs during render is an inter-procedural effects analysis rather than
	// anything syntactic. The rule's own doc comment measures the boundary and shows it is not a
	// statable rule: `const f = () => {g=1}; f()` reports upstream, `const f = () => {g=1};
	// return <Foo cb={f}/>` does not, and `return <Foo>{f}</Foo>` does again. `TestGlobalsBoundary`
	// pins the subset.
	//
	// Each of these was confirmed by running the real rule over the real golden through
	// `internal/reactconformancescore` and reading which construct it declined on, rather than
	// inferred from the doc comment.
	"error.assign-global-in-component-tag-function.js": {
		Fixture:  "error.assign-global-in-component-tag-function.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is inside a nested arrow, which verify's globals reports only for the compilation root's own body",
	},
	"error.assign-global-in-jsx-children.js": {
		Fixture:  "error.assign-global-in-jsx-children.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is inside a nested arrow passed as JSX children, which needs the effects pass to prove it runs during render",
	},
	"error.invalid-global-reassignment-indirect.js": {
		Fixture:  "error.invalid-global-reassignment-indirect.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is two nested arrows deep, reachable only through inter-procedural aliasing",
	},
	"error.reassign-global-fn-arg.js": {
		Fixture:  "error.reassign-global-fn-arg.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is inside a nested arrow passed to a function, which needs the effects pass",
	},
	"error.reassignment-to-global-indirect.js": {
		Fixture:  "error.reassignment-to-global-indirect.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is inside a nested arrow that is then called, which needs the effects pass",
	},

	// The compilation gate. verify's react rules fire only inside a function React Compiler would
	// compile, which for a `Component`-named function requires JSX or a hook call in the body. The
	// gate was measured over seventeen probe rounds against React's own rule and is shared with
	// `unsupported-syntax`. These fixtures are components by NAME with neither JSX nor a hook, so
	// upstream's test harness compiles them and verify's gate correctly does not.
	//
	// This is the one divergence here that is arguably worth closing, and it is recorded rather
	// than hidden for exactly that reason: it is invisible in application code, where a component
	// without JSX or hooks is not a component, and visible only against a corpus of minimal
	// repros.
	"error.reassignment-to-global.js": {
		Fixture:  "error.reassignment-to-global.js",
		Boundary: "TestGlobalsGateIsSharedWithUnsupportedSyntax",
		Reason:   "the function has neither JSX nor a hook call, so verify's compilation gate declines it while upstream's test harness compiles it anyway",
	},
	"new-mutability/error.reassignment-to-global.js": {
		Fixture:  "new-mutability/error.reassignment-to-global.js",
		Boundary: "TestGlobalsGateIsSharedWithUnsupportedSyntax",
		Reason:   "the function has neither JSX nor a hook call, so verify's compilation gate declines it",
	},
	"new-mutability/error.reassignment-to-global-indirect.js": {
		Fixture:  "new-mutability/error.reassignment-to-global-indirect.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "the write is inside a nested arrow that is then called, which needs the effects pass",
	},

	// Destructuring assignment to an undeclared target. Upstream reports these under Globals; the
	// write is a destructuring pattern rather than a simple or compound assignment.
	"error.invalid-destructure-assignment-to-global.js": {
		Fixture:  "error.invalid-destructure-assignment-to-global.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "an array-destructuring assignment whose target is an undeclared global, measured as declined by the rule",
	},
	"error.invalid-destructure-to-local-global-variables.js": {
		Fixture:  "error.invalid-destructure-to-local-global-variables.js",
		Boundary: "TestGlobalsBoundary",
		Reason:   "a mixed destructuring assignment where only one target is a global, measured as declined by the rule",
	},

	// The gate again, in its sharpest form. `useFoo` is hook-NAMED but calls no hook, and verify's
	// gate requires a hook CALL in the body, not a hook name on the function. The rule's own doc
	// comment states this directly at globals.go:195 (`function useFoo() { useState(0); g = 1; }
	// reports, hook`), and it was re-confirmed by probe here rather than read off that line:
	//
	//	function useFoo() { g += 1; return g; }        silent  <- this fixture
	//	function useFoo() { useState(0); g = 1; }      reports
	//	function Component() { g += 1; return <div/>; } reports
	//
	// The middle and bottom probes are the controls. Without them "the rule is silent" would be
	// consistent with the compound-assignment path being broken, which is a completely different
	// defect and the one worth ruling out — the doc comment makes a specific claim about `+=`
	// pointing at the whole assignment expression, so a silent `+=` had to be shown to be the gate
	// rather than the span logic.
	"error.update-global-should-bailout.tsx": {
		Fixture:  "error.update-global-should-bailout.tsx",
		Boundary: "TestGlobalsGateIsSharedWithUnsupportedSyntax",
		Reason:   "`useFoo` is hook-named but calls no hook, and verify's compilation gate requires a hook call in the body; probed with controls showing the same write reports from a function that does call one",
	},
}

// Classify decides which category a fixture belongs to for the rules verify ships.
//
// Order matters and is defended here, because each step short-circuits the ones below it and a
// different order produces a different number from the same inputs.
//
//  1. Flow first. A fixture verify cannot parse cannot be judged on any other axis, and calling it
//     anything else would be a claim about a file that was never read.
//  2. Stated divergence next, ahead of everything except parseability. A deliberate decision should
//     not be reclassified as a corpus artifact just because the fixture also happens to lack an
//     import; the decision is the more specific fact.
//  3. Unreachable, then no-rule-shipped. Both produce silence, and the distinction is whether the
//     question is askable at all.
//  4. Unresolvable types, but only for a type-aware rule. A syntax rule gets no such excuse.
//  5. Whatever the comparison says.
func Classify(fixture Fixture, result Result, err error) FixtureVerdict {
	rules, complete := fixture.Rules()
	verdict := FixtureVerdict{
		Name:     fixture.Name,
		Rules:    rules,
		Expected: fixture.Expected.Errors,
		Reported: result.Errors,
	}

	if fixture.RequiresFlow() {
		verdict.Verdict = VerdictFlowSyntax
		verdict.Reason = "requires a Flow parser (`@flow` in source, upstream's own test)"
		return verdict
	}

	if divergence, found := statedDivergences[fixture.Name]; found {
		verdict.Verdict = VerdictStatedDivergence
		verdict.Reason = divergence.Reason + " (held by " + divergence.Boundary + ")"
		return verdict
	}

	if !complete {
		// An unattributed diagnostic means the rule that owns it is the unknown, so scoring it
		// against any rule would credit or blame whichever happened to be running. This is
		// unreachable while TestEveryDiagnosticIsAttributed passes, and is kept as the honest
		// answer if it ever stops passing.
		verdict.Verdict = VerdictUnreachableFixture
		verdict.Reason = "not every expected diagnostic could be attributed to an upstream rule"
		return verdict
	}

	shipped := make([]string, 0, len(rules))
	for _, ruleName := range rules {
		if _, found := ShippedRules[ruleName]; found {
			shipped = append(shipped, ruleName)
		}
	}

	if len(shipped) == 0 {
		verdict.Verdict = VerdictNoRuleShipped
		verdict.Reason = "verify ships no rule for " + strings.Join(rules, ", ")
		return verdict
	}

	if err != nil {
		var unsupported *ErrUnsupported
		if asUnsupported(err, &unsupported) {
			// A decline from the implementation on a fixture a shipped rule owns is a statement
			// about the run, not about the fixture. Naming it `unreachable` would assert the
			// corpus cannot ask this question, which is the opposite of true: these are the only
			// fixtures it can ask.
			verdict.Verdict = VerdictNotScored
			verdict.Reason = unsupported.Reason
			return verdict
		}
		verdict.Verdict = VerdictFailed
		verdict.Reason = "analysis error: " + err.Error()
		return verdict
	}

	typeAware := false
	for _, ruleName := range shipped {
		if TypeAwareRules[ruleName] {
			typeAware = true
		}
	}
	if typeAware && len(result.Errors) == 0 && !fixture.ResolvesReactTypes() && fixture.CallsHook() {
		verdict.Verdict = VerdictUnresolvableTypes
		verdict.Reason = fmt.Sprintf(
			"%s reads types, and this fixture calls a hook without importing react, so every hook resolves to `any`",
			strings.Join(shipped, ", "))
		return verdict
	}

	comparison := Compare(fixture.Expected, result)
	if comparison.Outcome == OutcomePassed {
		verdict.Verdict = VerdictPassed
		return verdict
	}
	verdict.Verdict = VerdictFailed
	verdict.Reason = comparison.Detail
	return verdict
}

// Aggregate runs an implementation over the corpus and produces the categorised report.
func Aggregate(fixtures []Fixture, implementation Implementation) (Report, error) {
	for _, fixture := range fixtures {
		if err := CheckPragmas(fixture.Name, fixture.Pragmas); err != nil {
			return Report{}, err
		}
	}

	report := Report{Considered: len(fixtures), Counts: map[Verdict]int{}}
	for _, fixture := range fixtures {
		var result Result
		var err error
		if !fixture.RequiresFlow() {
			result, err = implementation.Analyze(fixture)
		}
		verdict := Classify(fixture, result, err)
		report.Counts[verdict.Verdict]++
		report.Fixtures = append(report.Fixtures, verdict)
	}

	sort.Slice(report.Fixtures, func(first, second int) bool {
		return report.Fixtures[first].Name < report.Fixtures[second].Name
	})
	if err := report.Check(); err != nil {
		return Report{}, err
	}
	return report, nil
}
