package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// LastUsage records the last instruction at which each declaration is read.
//
// Upstream's `FindLastUsageVisitor`. Keyed by `DeclarationId` rather than `IdentifierId`, which is
// upstream's choice and carries its own `TODO` saying so: declaration-keying is what makes the output
// compatible, and it is deliberately imprecise where a scope defines a variable whose version is
// never read and is overwritten later. Do not "fix" this to `IdentifierId` -- upstream's note says
// that is correct only once the pass leaves single-assignment form behind, which it has not.
type LastUsage struct {
	byDeclaration map[static_single_assignment.DeclarationId]static_single_assignment.EvaluationOrder
}

// LastUsedAt returns the last order at which a declaration was read, and whether it was read at all.
//
// The second return is not decoration. Upstream indexes its map with a non-null assertion and would
// throw on a miss; a linter cannot, and the two callers treat a miss differently -- one skips the
// declaration, the other declines the merge.
func (usage *LastUsage) LastUsedAt(
	declaration static_single_assignment.DeclarationId,
) (static_single_assignment.EvaluationOrder, bool) {
	if usage == nil || usage.byDeclaration == nil {
		return 0, false
	}
	order, found := usage.byDeclaration[declaration]
	return order, found
}

// Length reports how many declarations were seen, for measurement.
func (usage *LastUsage) Length() int {
	if usage == nil {
		return 0
	}
	return len(usage.byDeclaration)
}
