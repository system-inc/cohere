package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// HoistableAnalysis is what this pass produces: the non-null accesses at every block, and whether
// the analysis actually converged.
//
// `Converged` is the field that does not exist upstream in a readable form, and it is the point of
// this type. React throws when the loop hits the cap; oxc silently exits and returns a truncated
// answer indistinguishable from a real one. A linter can do neither, so it reports.
type HoistableAnalysis struct {
	blocks    map[static_single_assignment.BlockId]map[int]bool
	registry  *pathRegistry
	converged bool
	// iterations is how many outer passes ran, for measuring how close real code comes to the cap.
	iterations int
}

// Converged reports whether the fixed point was reached inside the iteration cap.
//
// A false result means the answer is partial: correct as far as it goes, because every pass only
// adds proven-non-null accesses, but potentially missing some. A caller that needs upstream's
// behaviour treats false as an error; a caller that prefers a shallower dependency treats it as a
// smaller hoistable set, which is the safe direction.
func (analysis *HoistableAnalysis) Converged() bool {
	return analysis != nil && analysis.converged
}

// Iterations reports how many outer passes ran, for measurement.
func (analysis *HoistableAnalysis) Iterations() int {
	if analysis == nil {
		return 0
	}
	return analysis.iterations
}

// hoistableAt returns the non-null access paths proven at one block.
func (analysis *HoistableAnalysis) hoistableAt(block static_single_assignment.BlockId) []ReactiveScopeDependency {
	if analysis == nil || analysis.blocks == nil {
		return nil
	}
	set, ok := analysis.blocks[block]
	if !ok {
		return nil
	}
	paths := make([]ReactiveScopeDependency, 0, len(set))
	for _, index := range sortedNodeIndices(set) {
		paths = append(paths, analysis.registry.nodes[index].fullPath)
	}
	return paths
}
