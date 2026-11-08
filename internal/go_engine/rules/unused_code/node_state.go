package unused_code

// nodeState is what coloring learned of a node: whether the root reaches it, and whether it is quiet.
type nodeState uint8

const (
	nodeStateSeen nodeState = 1 << iota
	nodeStateQuiet
)

// seen is whether the root reaches the node.
//
//gcassert:inline
func (state nodeState) seen() bool { return state&nodeStateSeen != 0 }

// quiet is whether an unseen node owns the node, so it goes unreported.
//
//gcassert:inline
func (state nodeState) quiet() bool { return state&nodeStateQuiet != 0 }
