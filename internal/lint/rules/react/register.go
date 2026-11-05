// Package react holds the ported `eslint-plugin-react` rules.
//
// Named for the plugin whose namespace the inventory writes (`react/no-children-prop`), and kept
// separate from `internal/rules/structure/`, which also carries React judgments. The split is by
// who decided the rule rather than by what it is about: everything here reproduces a judgment
// upstream already makes and is measured against upstream's own corpus, while `structure` holds
// judgments this codebase made for itself and has no upstream to check against. Merging them would
// lose the ability to say which findings a differential run is allowed to disagree about.
//
// Distinct from `internal/utilities/react/` despite the name, which holds the shared predicates these
// rules ask (`IsCreateElementCall`, `EnclosingComponent`) and is imported by rule packages rather
// than being one.
package react

import (
	"github.com/system-inc/cohere/internal/lint/rule"
)

// init registers this package's rules with the catalog.
//
// Registration lives beside the rules rather than in a shared list another package maintains. That
// shared list was one file every port had to edit, so two ports landing at once conflicted on a
// file neither was really changing, and the conflict scaled with how many rules were being written
// at the same time. A rule is now two new files and no shared edit.
//
// The cost is that a package whose rules are never imported registers nothing, so the catalog would
// be silently short. `registry` imports every rule package for exactly that reason, and the parity
// guard reads the result against the inventory, so a package dropped from those imports shows up as
// rules that vanished rather than as a quieter run.
func init() {
	rule.Register(
		rule.Registration[CompilerRuleOptions]{Rule: Config, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: ErrorBoundaries, Decode: DecodeCompilerRuleOptions},
		rule.Registration[ExhaustiveDepsOptions]{
			Rule:   ExhaustiveDeps,
			Decode: rule.DecodeOptionsInto[ExhaustiveDepsOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: ForwardRefUsesRef},
		rule.Registration[GatingOptions]{Rule: Gating, Decode: rule.DecodeOptionsInto[GatingOptions]()},
		rule.Registration[CompilerRuleOptions]{Rule: Globals, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: Immutability, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: IncompatibleLibrary, Decode: DecodeCompilerRuleOptions},
		rule.Registration[JsxNoDuplicatePropsOptions]{
			Rule:   JsxNoDuplicateProps,
			Decode: rule.DecodeOptionsInto[JsxNoDuplicatePropsOptions](),
		},
		rule.Registration[JsxNoUndefOptions]{Rule: JsxNoUndef, Decode: rule.DecodeOptionsInto[JsxNoUndefOptions]()},
		rule.Registration[rule.NoOptions]{Rule: JsxPropsNoSpreadMulti},
		rule.Registration[NoChildrenPropOptions]{
			Rule:   NoChildrenProp,
			Decode: rule.DecodeOptionsInto[NoChildrenPropOptions](),
		},
		rule.Registration[rule.NoOptions]{Rule: NoDangerWithChildren},
		rule.Registration[NoMethodSetStateOptions]{Rule: NoDidMountSetState, Decode: DecodeNoMethodSetStateOptions},
		rule.Registration[NoMethodSetStateOptions]{Rule: NoDidUpdateSetState, Decode: DecodeNoMethodSetStateOptions},
		rule.Registration[rule.NoOptions]{Rule: NoDirectMutationState},
		rule.Registration[rule.NoOptions]{Rule: NoFindDOMNode},
		rule.Registration[rule.NoOptions]{Rule: NoIsMounted},
		rule.Registration[rule.NoOptions]{Rule: NoRenderReturnValue},
		rule.Registration[rule.NoOptions]{Rule: NoThisInSfc},
		rule.Registration[NoUnsafeOptions]{Rule: NoUnsafe, Decode: rule.DecodeOptionsInto[NoUnsafeOptions]()},
		rule.Registration[NoMethodSetStateOptions]{Rule: NoWillUpdateSetState, Decode: DecodeNoMethodSetStateOptions},
		rule.Registration[CompilerRuleOptions]{Rule: Purity, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: Refs, Decode: DecodeCompilerRuleOptions},
		rule.Registration[RulesOfHooksOptions]{
			Rule:   RulesOfHooks,
			Decode: rule.DecodeOptionsInto[RulesOfHooksOptions](),
		},
		rule.Registration[CompilerRuleOptions]{Rule: SetStateInEffect, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: SetStateInRender, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: StaticComponents, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: UnsupportedSyntax, Decode: DecodeCompilerRuleOptions},
		rule.Registration[CompilerRuleOptions]{Rule: UseMemo, Decode: DecodeCompilerRuleOptions},
		rule.Registration[rule.NoOptions]{Rule: VoidDomElementsNoChildren},
		rule.Registration[CompilerRuleOptions]{Rule: VoidUseMemo, Decode: DecodeCompilerRuleOptions},
	)
}
