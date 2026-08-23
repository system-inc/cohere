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
	"github.com/system-inc/verify/internal/rules/nexus"
)

// All returns every rule, in a stable order.
//
// Order is stable so that two runs over the same tree report findings in the same sequence, which
// is what makes a diff between them meaningful.
func All() []rule.Rule {
	return []rule.Rule{
		nexus.BoundaryNoInternalImport,
		nexus.BoundaryNoNexusOutsideImport,
		nexus.BoundaryNoProjectImport,
		nexus.ConsistencyNoAmbiguousIdentifier,
		nexus.ConsistencyNoBooleanOutcome,
		nexus.ConsistencyNoEnum,
		nexus.ConsistencyNoLongLineComment,
		nexus.ConsistencyNoMultilineArrowFunction,
		nexus.ConsistencyNoScreamingSnakeCase,
		nexus.ConsistencyNoShouting,
		nexus.ConsistencyNoSingleLineJsDoc,
		nexus.ConsistencyNoStrictUndefinedAstCheck,
		nexus.ConsistencyNoStutteringName,
		nexus.ConsistencyNoUtilsFolder,
		nexus.ConsistencyRequireTypeSuffix,
		nexus.ImportNoForbiddenSource,
		nexus.ImportRequireNodeNamespace,
		nexus.ImportRequirePathAlias,
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

		// The rest tune behavior rather than enable it, so they run on their own defaults when the
		// config says nothing.
		"consistency-no-boolean-outcome":      {Decode: config.DecodeInto[nexus.ConsistencyNoBooleanOutcomeOptions]()},
		"consistency-no-long-line-comment":    {Decode: config.DecodeInto[nexus.ConsistencyNoLongLineCommentOptions]()},
		"consistency-no-screaming-snake-case": {Decode: config.DecodeInto[nexus.ConsistencyNoScreamingSnakeCaseOptions]()},
		"consistency-no-shouting":             {Decode: config.DecodeInto[nexus.ConsistencyNoShoutingOptions]()},
		"consistency-no-stuttering-name":      {Decode: config.DecodeInto[nexus.ConsistencyNoStutteringNameOptions]()},
	}
}
