package build_dependence

import (
	"go/types"
)

// Against judges one extra build against the host: the seeds its files give (rules 1 and 2), the two-bit fixed point
// over the graph's kind edges (rule 3), and each unit's verdict, by its declared objects and its body's references
// (rule 4). It reads the graph and never changes it, so several builds can be judged at once.
func (graph *Graph) Against(difference Difference) *Dependence {
	resolved := graph.resolve()
	judging := &judgment{
		resolved:   resolved,
		difference: difference,
		seedUnit:   make([]bool, resolved.unitCount()),
		answers:    map[types.Object]Bits{},
	}
	judging.seedFiles()
	judging.seedMethodSets()
	judging.seedShadowed()
	for _, edge := range resolved.externalEdges {
		if judging.answer(edge.to) {
			judging.seedBits = append(judging.seedBits, edge.fromBit)
		}
	}
	bits := resolved.fixedPoint(judging.seedBits)

	unitDependent := make([]bool, resolved.unitCount())
	for index := range unitDependent {
		unitDependent[index] = judging.seedUnit[index] || judging.unitReaches(index, bits)
	}
	dependence := &Dependence{resolved: resolved, judging: judging, unitDependent: unitDependent, bits: bits}
	dependence.counts = judging.count(unitDependent)
	return dependence
}
