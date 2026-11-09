package intermediate_representation

// Walk visits block and every block reachable from it through successors, each once, depth first. visit returning
// false stops the walk below that block, not the whole walk: the blocks still waiting are visited. It is
// staticcheck's irutil.Walk (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR, so a reader
// moving from go/ir visits the same blocks in the same order through it.
func Walk(block *BasicBlock, visit func(*BasicBlock) bool) {
	seen := map[*BasicBlock]bool{}
	waiting := []*BasicBlock{block}
	for len(waiting) > 0 {
		next := waiting[len(waiting)-1]
		waiting = waiting[:len(waiting)-1]
		if seen[next] {
			continue
		}
		seen[next] = true
		if !visit(next) {
			continue
		}
		waiting = append(waiting, next.Successors...)
	}
}
