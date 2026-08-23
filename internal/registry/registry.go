// Package registry is the list of every rule verify knows.
//
// Rules are compiled in rather than loaded, which is what makes them free to run: a rule walks the
// same AST the parser already built, in the same address space, so the five hundredth rule costs
// what the hundredth does. It also means a rule present in the source cannot be silently absent at
// runtime, which a configuration-driven plugin system cannot promise. During the migration this
// tool replaces, three configurations ran successfully having loaded zero plugins.
package registry

import (
	"github.com/system-inc/verify/internal/config"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/core"
	"github.com/system-inc/verify/internal/rules/next"
	"github.com/system-inc/verify/internal/rules/nexus"
	"github.com/system-inc/verify/internal/rules/structure"
	"github.com/system-inc/verify/internal/rules/tailwind"
)

// All returns every rule, in a stable order.
//
// Order is stable so that two runs over the same tree report findings in the same sequence, which
// is what makes a diff between them meaningful.
func All() []rule.Rule {
	return []rule.Rule{
		core.NoCaseDeclarations,
		core.NoCompareNegZero,
		core.NoDebugger,
		core.NoDeleteVar,
		core.NoDuplicateCase,
		core.NoEmpty,
		core.NoEmptyPattern,
		core.NoEmptyStaticBlock,
		core.NoExAssign,
		core.NoSparseArrays,
		core.NoUnsafeFinally,
		core.NoVar,
		core.NoUselessCatch,
		core.PreferSpread,
		core.RequireYield,
		core.UseIsNaN,
		core.ValidTypeof,
		next.NoAssignModuleVariable,
		nexus.BoundaryNoInternalImport,
		nexus.BoundaryNoNexusOutsideImport,
		nexus.BoundaryNoProjectImport,
		nexus.ConsistencyNoAbbreviatedIdentifier,
		nexus.ConsistencyNoAmbiguousIdentifier,
		nexus.ConsistencyNoBooleanOutcome,
		nexus.ConsistencyNoEnum,
		nexus.ConsistencyNoLongLineComment,
		nexus.ConsistencyNoMultilineArrowFunction,
		nexus.ConsistencyNoScreamingSnakeCase,
		nexus.ConsistencyNoShouting,
		nexus.ConsistencyNoSingleLineJsDoc,
		nexus.ConsistencyNoStutteringName,
		nexus.ConsistencyNoUtilsFolder,
		nexus.ConsistencyRequireConstantCasing,
		nexus.ConsistencyRequireTypeSuffix,
		nexus.ImportNoForbiddenSource,
		nexus.ImportRequireModuleAlias,
		nexus.ImportRequireNodeNamespace,
		nexus.ImportRequirePathAlias,
		nexus.LocalizationNoUntranslatedValue,
		structure.NetworkNoDirectFetch,
		structure.NetworkNoForbiddenImport,
		structure.NetworkNoInvalidateCacheInOnSuccess,
		structure.NetworkNoInvalidateCacheLiteralKey,
		structure.NetworkNoStringLiteralQuery,
		structure.NetworkRequireHookOptionsParameter,
		structure.NetworkRequireHookRequestSuffix,
		structure.NetworkRequireHookVariablesType,
		structure.ReactComponentNoForwardRef,
		structure.ReactComponentNoMultiplePrimary,
		structure.ReactNoAnchorElement,
		structure.ReactNoHorizontalRuleElement,
		tailwind.NoConcatenatedClasses,
		tailwind.NoDuplicateClasses,
		tailwind.NoUnnecessaryWhitespace,
	}
}

// Count is how many rules exist, for the coverage line.
func Count() int {
	return len(All())
}

// Options says how to decode each rule's configuration, and which rules cannot run without it.
//
// A rule declares its own options struct and only its own package knows that type, while the config
// layer holds JSON. This map is the seam between them.
//
// Required is the field that matters. `boundary-no-project-import` was enabled and inert for months
// under the gate verify replaces: it declines every file when LibraryDirectory is empty, which is
// correct behavior for a misconfigured guard and indistinguishable from a rule with nothing to
// report. A liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that ever
// caught it. Marking it Required turns that silence into a failure.
//
// A rule absent from this map takes no options, which is the common case and needs no entry.
func Options() config.OptionsRegistry {
	return config.OptionsRegistry{
		// Required: the rule guards one library directory and declines everything without it.
		"boundary-no-project-import": {
			Decode:   config.DecodeInto[nexus.BoundaryNoProjectImportOptions](),
			Required: true,
		},

		// Required for the same reason: without aliases there is nothing to suggest, and without a
		// repository root no path can be made relative, so the rule declines every file. The
		// TypeScript original reads process.cwd() for that root, which made its verdict depend on
		// where the linter was invoked from. Passing it explicitly turns an invisible dependency
		// into a configuration error.
		"import-require-path-alias": {
			Decode:   config.DecodeInto[nexus.ImportRequirePathAliasOptions](),
			Required: true,
		},

		// Not Required. The option only relaxes the rule, and its default is the strict reading, so
		// a config that says nothing gets the full rule rather than a rule that reads no files.
		"no-empty": {Decode: config.DecodeInto[core.NoEmptyOptions]()},

		// Not Required, and the default is the strict reading. enforceForSwitchCase defaults to
		// true, matching ESLint 9 and this tree's config, so a config that says nothing gets the
		// whole rule rather than the comparison half of it.
		"use-isnan":        {Decode: config.DecodeInto[core.UseIsNaNOptions]()},
		"no-empty-pattern": {Decode: config.DecodeInto[core.NoEmptyPatternOptions]()},

		// The rest tune behavior rather than enable it, so they run on their own defaults when the
		// config says nothing.
		"consistency-no-abbreviated-identifier": {
			Decode: config.DecodeInto[nexus.ConsistencyNoAbbreviatedIdentifierOptions](),
		},
		"consistency-no-boolean-outcome":      {Decode: config.DecodeInto[nexus.ConsistencyNoBooleanOutcomeOptions]()},
		"consistency-no-long-line-comment":    {Decode: config.DecodeInto[nexus.ConsistencyNoLongLineCommentOptions]()},
		"consistency-no-screaming-snake-case": {Decode: config.DecodeInto[nexus.ConsistencyNoScreamingSnakeCaseOptions]()},
		"consistency-no-shouting":             {Decode: config.DecodeInto[nexus.ConsistencyNoShoutingOptions]()},
		"consistency-no-stuttering-name":      {Decode: config.DecodeInto[nexus.ConsistencyNoStutteringNameOptions]()},

		// Not Required. Both thresholds have defaults that match the gate verify replaces, so a
		// config that says nothing gets the real rule rather than a rule that reads no files.
		"react-component-no-multiple-primary": {
			Decode: config.DecodeInto[structure.ReactComponentNoMultiplePrimaryOptions](),
		},

		// Not Required. The option only exempts names a framework reads verbatim, so a config that
		// says nothing gets the full rule rather than a rule that reads no files.
		"consistency-require-constant-casing": {
			Decode: config.DecodeInto[nexus.ConsistencyRequireConstantCasingOptions](),
		},
		"import-require-module-alias": {Decode: config.DecodeInto[nexus.ImportRequireModuleAliasOptions]()},

		// Not Required, deliberately. The surfaces that carry class strings have sane defaults
		// (`class`/`className`, the two merge helpers, the `*ClassName` variable patterns), and a
		// project that says nothing gets those rather than a rule that reads no files. The option
		// exists to widen the surface, not to enable the rule.
		"no-concatenated-classes":   {Decode: config.DecodeInto[tailwind.NoConcatenatedClassesOptions]()},
		"no-duplicate-classes":      {Decode: config.DecodeInto[tailwind.NoDuplicateClassesOptions]()},
		"no-unnecessary-whitespace": {Decode: config.DecodeInto[tailwind.NoUnnecessaryWhitespaceOptions]()},
	}
}
