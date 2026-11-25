package forward_flow

import "honnef.co/go/tools/analysis/dfa"

// nodeSet is a set of nodes numbered below 64, one bit each.
type nodeSet uint64

// nodeSetUnion is the semilattice of nodeSet under union, upstream's test lattice.
type nodeSetUnion struct{}

var _ dfa.Semilattice[nodeSet] = nodeSetUnion{}

func (nodeSetUnion) Ident() nodeSet { return 0 }

func (nodeSetUnion) Equals(left nodeSet, right nodeSet) bool { return left == right }

func (nodeSetUnion) Merge(left nodeSet, right nodeSet) nodeSet { return left | right }

// setOf is the set of nodes.
func setOf(nodes ...int) nodeSet {
	var set nodeSet
	for _, node := range nodes {
		if node < 0 || node >= 64 {
			// Invariant: every test names nodes below 64, which is all a nodeSet holds.
			panic("forward_flow: a node out of a nodeSet's range")
		}
		set |= 1 << node
	}
	return set
}
