// Package registry is the list of every rule verify knows.
//
// Rules are compiled in rather than loaded, which is what makes them free to run: a rule walks the
// same AST the parser already built, in the same address space, so the five hundredth rule costs
// what the hundredth does. It also means a rule present in the source cannot be silently absent at
// runtime, which a configuration-driven plugin system cannot promise. During the migration this
// tool replaces, three configurations ran successfully having loaded zero plugins.
package registry

import (
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
		nexus.ConsistencyNoBooleanOutcome,
		nexus.ConsistencyNoEnum,
		nexus.ConsistencyNoLongLineComment,
		nexus.ConsistencyNoScreamingSnakeCase,
		nexus.ConsistencyNoShouting,
		nexus.ConsistencyNoSingleLineJsDoc,
		nexus.ConsistencyNoStrictUndefinedAstCheck,
		nexus.ConsistencyNoStutteringName,
		nexus.ConsistencyNoUtilsFolder,
		nexus.ConsistencyRequireTypeSuffix,
		nexus.ImportNoForbiddenSource,
		nexus.ImportRequireNodeNamespace,
	}
}

// Count is how many rules exist, for the coverage line.
func Count() int {
	return len(All())
}
