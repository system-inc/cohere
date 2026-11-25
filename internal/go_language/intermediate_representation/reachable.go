package intermediate_representation

// Reachable reports whether control can pass from block from to block to: from is to, from dominates to, or a walk
// over successors from from meets to. It is staticcheck's irutil.Reachable (honnef.co/go/tools@v0.8.1
// go/ir/irutil/util.go), translated onto our IR, so a reader moving from go/ir finds the same blocks reachable
// through it.
func Reachable(from, to *BasicBlock) bool {
	if from == to {
		return true
	}
	// A block dominates only blocks it reaches, so dominance answers without a walk.
	if from.Dominates(to) {
		return true
	}
	found := false
	Walk(from, func(block *BasicBlock) bool {
		if block == to {
			found = true
			return false
		}
		return true
	})
	return found
}
