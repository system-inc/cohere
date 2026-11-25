package build_dependence

// Against judges one extra build against the host: the seeds its files give (rules 1 and 2), the two-bit fixed point
// over the graph's kind edges (rule 3), and each unit's verdict, by its declared objects and its body's references
// (rule 4). It reads the graph and never changes it, so several builds can be judged at once. A declarations-only graph
// (NewDeclarationsGraph) is judged as far as its bits: it holds no unit's reads, so no unit is given a verdict and none
// is counted, both left unset, and every reader of either refuses (refuseDeclarationsOnly).
func (graph *Graph) Against(difference Difference) *Dependence {
	resolved := graph.resolve()
	judging := &judgment{
		resolved:   resolved,
		difference: difference,
		seedUnit:   make([]bool, resolved.unitCount()),
		answers:    make([]uint8, len(resolved.externalObjects)),
	}
	judging.seedFiles()
	judging.seedWithheld()
	judging.seedMethodSets()
	judging.seedShadowed()
	for _, edge := range resolved.externalEdges {
		if judging.answer(edge.to, nil) {
			judging.seedBits = append(judging.seedBits, edge.fromBit)
		}
	}
	bits := resolved.fixedPoint(judging.seedBits)

	dependence := &Dependence{resolved: resolved, judging: judging, bits: bits,
		declarationsOnly: graph.declarationsOnly}
	if graph.declarationsOnly {
		return dependence
	}
	dependence.unitDependent = make([]bool, resolved.unitCount())
	for index := range dependence.unitDependent {
		dependence.unitDependent[index] = judging.seedUnit[index] || judging.unitReaches(index, bits)
	}
	dependence.counts = judging.count(dependence.unitDependent)
	return dependence
}
