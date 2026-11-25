package control_flow_graph

// liveNames is a Backward "may" analysis: the set of event names that appear anywhere later on some
// path. It is the shape of liveness with the symbol table replaced by names, so it exercises the
// backward direction and the exit seeding without dragging in a checker.
type liveNames struct{}

func (liveNames) Bottom() map[string]bool { return map[string]bool{} }

func (liveNames) Entry() map[string]bool { return map[string]bool{} }

func (liveNames) Meet(left, right map[string]bool) map[string]bool {
	return reachingNames{}.Meet(left, right)
}

func (liveNames) Transfer(block *Block[string], incoming map[string]bool) map[string]bool {
	outgoing := make(map[string]bool, len(incoming)+len(block.Events))
	for name := range incoming {
		outgoing[name] = true
	}
	for _, event := range block.Events {
		outgoing[event] = true
	}
	return outgoing
}

func (liveNames) Equal(left, right map[string]bool) bool {
	return reachingNames{}.Equal(left, right)
}
