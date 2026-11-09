package return_nilness

import "honnef.co/go/tools/analysis/dfa"

// lattice is the semilattice of ValueNilness, each field merged on its own by latticeMerge, with no knowledge as its
// identity.
type lattice struct{}

var _ dfa.Semilattice[ValueNilness] = lattice{}

// Equals implements dfa.Semilattice.
func (lattice) Equals(left ValueNilness, right ValueNilness) bool {
	return left == right
}

// Ident implements dfa.Semilattice.
func (lattice) Ident() ValueNilness {
	return ValueNilness{}
}

// latticeMerge is the merge of two Nilness values, staticcheck's table entry for entry: no knowledge gives way to
// anything, and two that differ become MaybeNil, except that NeverNil and MaybeNilGlobal merge to MaybeNilGlobal.
var latticeMerge = [5][5]Nilness{
	0: {
		0:              0,
		NeverNil:       NeverNil,
		AlwaysNil:      AlwaysNil,
		MaybeNilGlobal: MaybeNilGlobal,
		MaybeNil:       MaybeNil,
	},
	NeverNil: {
		0:              NeverNil,
		NeverNil:       NeverNil,
		AlwaysNil:      MaybeNil,
		MaybeNilGlobal: MaybeNilGlobal,
		MaybeNil:       MaybeNil,
	},
	AlwaysNil: {
		0:              AlwaysNil,
		NeverNil:       MaybeNil,
		AlwaysNil:      AlwaysNil,
		MaybeNilGlobal: MaybeNil,
		MaybeNil:       MaybeNil,
	},
	MaybeNilGlobal: {
		0:              MaybeNilGlobal,
		NeverNil:       MaybeNilGlobal,
		AlwaysNil:      MaybeNil,
		MaybeNilGlobal: MaybeNilGlobal,
		MaybeNil:       MaybeNil,
	},
	MaybeNil: {
		0:              MaybeNil,
		NeverNil:       MaybeNil,
		AlwaysNil:      MaybeNil,
		MaybeNilGlobal: MaybeNil,
		MaybeNil:       MaybeNil,
	},
}

// Merge implements dfa.Semilattice.
func (lattice) Merge(left ValueNilness, right ValueNilness) ValueNilness {
	return ValueNilness{
		Inner: latticeMerge[left.Inner][right.Inner],
		Outer: latticeMerge[left.Outer][right.Outer],
	}
}
