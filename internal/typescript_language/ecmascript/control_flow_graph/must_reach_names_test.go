package control_flow_graph

// mustReachNames is the "must" counterpart: the names seen on every path to this point. Meet is
// intersection, so Bottom has to be the universe rather than the empty set, which is the case the
// LatticeOf doc warns reads backward.
type mustReachNames struct{ universe map[string]bool }

func (names mustReachNames) Bottom() map[string]bool {
	all := make(map[string]bool, len(names.universe))
	for name := range names.universe {
		all[name] = true
	}
	return all
}

func (mustReachNames) Entry() map[string]bool { return map[string]bool{} }

func (mustReachNames) Meet(left, right map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for name := range left {
		if right[name] {
			merged[name] = true
		}
	}
	return merged
}

func (mustReachNames) Transfer(block *Block[string], incoming map[string]bool) map[string]bool {
	outgoing := make(map[string]bool, len(incoming)+len(block.Events))
	for name := range incoming {
		outgoing[name] = true
	}
	for _, event := range block.Events {
		outgoing[event] = true
	}
	return outgoing
}

func (mustReachNames) Equal(left, right map[string]bool) bool {
	return reachingNames{}.Equal(left, right)
}
