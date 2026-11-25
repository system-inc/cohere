package global_rename

import "go/ast"

// flowState is one origin's walk: what it has visited and what it found.
type flowState struct {
	ownRead     bool
	heldRead    bool
	seenObjects map[Key]flowMode
	seenResults map[Key]flowMode
	seenSites   map[ast.Expr]flowMode
	pending     []flowItem
}

// read records that the value read a type's name, or left for code that might.
func (state *flowState) read(mode flowMode) {
	if mode&flowOwn != 0 {
		state.ownRead = true
	} else {
		state.heldRead = true
	}
}
