package main

type groupPlan struct {
	removals map[*sourceFile][]byteRange
	// appends holds, per destination file name, the chunks to add at its end, in order.
	appends map[string][]chunk
	// created maps each new file's name to the file whose build lines it takes.
	created map[string]*sourceFile
}

// receiverOf is the file that took the first of name's code, for a file the split emptied.
func (plan *groupPlan) receiverOf(name string) string {
	for _, target := range sortedNames(plan.appendTargets()) {
		for _, piece := range plan.appends[target] {
			if piece.source.name == name {
				return target
			}
		}
	}
	return name
}

// appendTargets is the names plan appends to, as a set.
func (plan *groupPlan) appendTargets() map[string][]byte {
	targets := map[string][]byte{}
	for target := range plan.appends {
		targets[target] = nil
	}
	return targets
}
