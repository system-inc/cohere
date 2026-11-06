package correctness_require_declared_walk

type origins map[origin]bool

// add puts every origin of more into into, and says whether into grew.
func (into origins) add(more origins) bool {
	grew := false
	for incoming := range more {
		if !into[incoming] {
			into[incoming] = true
			grew = true
		}
	}
	return grew
}
