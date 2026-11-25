package build_dependence

import (
	"go/types"
)

// IdentifiedObjects answers a build's bits of the live objects of packages the graph holds by records read back from
// the store (#6a3kjvz), whose records hold no live object for ObjectBits to find: each object by its package's ID and
// its name there, as a record names it (localName). A path names no one package where the graph holds a package beside
// its test variants, which share it, so a name is never asked by its path (NameBits) here: the stage 2 reach asks of
// the host's packages, test variants among them (#w72zxtq). An object whose package has no ID, or whose name a record
// could not hold, answers both bits set, failing wide.
//
// It is one build's, asked from one goroutine at a time, as ObjectBits is.
type IdentifiedObjects struct {
	judged      *Dependence
	identifiers map[*types.Package]string
	paths       objectPaths
	walker      *bitWalker
}

// NewIdentifiedObjects answers by judged, the build's judgment, each object found by identifiers, the ID of each
// package the graph's packages were loaded as, by its types.
func NewIdentifiedObjects(judged *Dependence, identifiers map[*types.Package]string) *IdentifiedObjects {
	return &IdentifiedObjects{judged: judged, identifiers: identifiers, walker: &bitWalker{seen: map[walkKey]bool{}}}
}

// Bits are object's bits in the build: a live object a unit declares has its own; one a decoded record's unit
// declares, found by its package's ID and its name, that unit's; and one no unit declares is read through its type,
// both bits set when any bit its type reaches is, as ObjectBits reads it. An object of a package the graph does not
// hold is answered as ObjectBits answers it.
func (identified *IdentifiedObjects) Bits(object types.Object) Bits {
	declared := canonical(object)
	if universal(declared) {
		return Bits{}
	}
	set, isDeclared, isKnown := identified.bit(declared, false)
	switch {
	case !isKnown:
		return Bits{Name: true, Underlying: true}
	case isDeclared:
		underlying, _, _ := identified.bit(declared, true)
		return Bits{Name: set, Underlying: underlying}
	}
	for _, through := range identified.walker.borrowedThroughReferences(declared) {
		set, _, isKnown := identified.bit(through.object, through.touchesUnderlying)
		if set || !isKnown {
			return Bits{Name: true, Underlying: true}
		}
	}
	return Bits{}
}

// bit is object's name bit, or with underlying its underlying bit, whether a unit of the graph declares object, and
// whether the object could be named to one package at all. An object of a package the graph does not hold is the
// build's DependentObject's to answer, and counts as declared, its answer final.
func (identified *IdentifiedObjects) bit(object types.Object, underlying bool) (bool, bool, bool) {
	resolved := identified.judged.resolved
	if node, isNode := resolved.objectNodes[object]; isNode {
		return identified.judged.bits[bit(node, underlying)], true, true
	}
	if resolved.typesPackages[object.Pkg()] {
		return false, false, true
	}
	identifier, isIdentified := identified.identifiers[object.Pkg()]
	if !isIdentified {
		return false, false, false
	}
	packageIndex, isHeld := resolved.packageIndex[identifier]
	if !isHeld {
		return identified.judged.judging.answerObject(object, underlying), true, true
	}
	local := localName(&identified.paths, object)
	if local == "" {
		return false, false, false
	}
	node, isNode := resolved.nodes[nodeKey{packageIndex: int32(packageIndex), local: local}]
	if !isNode {
		return false, false, true
	}
	return identified.judged.bits[bit(node, underlying)], true, true
}
