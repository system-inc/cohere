package intermediate_representation

// Flatten is the one value value stands for through every phi it passes: value itself when it is no phi, the value
// every edge flattens to when they all agree, and nil when two edges bring different values. A phi met again on the
// way, as a loop's phi is, adds nothing. It is staticcheck's irutil.Flatten (honnef.co/go/tools@v0.8.1
// go/ir/irutil/util.go), translated onto our IR, so a reader moving from go/ir reads the same value through it.
func Flatten(value Value) Value {
	failed := false
	seen := map[Value]bool{}
	var flattened Value
	var visit func(visited Value)
	visit = func(visited Value) {
		if failed || seen[visited] {
			return
		}
		seen[visited] = true
		phi, isPhi := visited.(*Phi)
		if isPhi {
			for _, edge := range phi.Edges {
				visit(edge)
			}
			return
		}
		if flattened == nil {
			flattened = visited
		} else if flattened != visited {
			failed = true
		}
	}
	visit(value)
	if failed {
		return nil
	}
	return flattened
}
