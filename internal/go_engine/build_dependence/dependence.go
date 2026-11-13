package build_dependence

import (
	"go/types"
	"sort"
)

// Dependence is one build's judgment of the graph's units and objects (Against).
type Dependence struct {
	resolved *resolvedGraph
	// judging is the judgment the build was judged by, whose answers ObjectBits reads for an object of a package the
	// graph does not hold.
	judging       *judgment
	unitDependent []bool
	bits          []bool
	counts        Counts
}

// Bits are an object's two bits in one build. A named type has both; any other object only Name.
type Bits struct {
	Name       bool
	Underlying bool
}

// Counts are one build's counts, by scope (see Against's count).
type Counts struct {
	Workspace    ScopeCounts
	Dependencies ScopeCounts
}

// UnitDependent reports whether the unit of the package packageId that starts at offset is build-dependent: file is the
// one a reader sees, as the unit's start's position names it past any //line directive, and offset is in the file the
// host parsed. A unit the graph does not hold, one in a file only the build compiles, is
// dependent by definition, and any other place is no unit the graph knows, which is never read as independent.
func (dependence *Dependence) UnitDependent(packageId string, file string, offset int) bool {
	index, isUnit := dependence.resolved.unitIndex[unitKey{packageIdentifier: packageId, file: file, offset: offset}]
	if !isUnit {
		return true
	}
	return dependence.unitDependent[index]
}

// Counts are the build's counts.
func (dependence *Dependence) Counts() Counts {
	return dependence.counts
}

// DependentObjects are the objects the build makes dependent, each as its generic declares it, with its bits, so one
// graph's answer can feed another's Difference.DependentObject.
func (dependence *Dependence) DependentObjects() map[types.Object]Bits {
	dependent := map[types.Object]Bits{}
	for node, object := range dependence.resolved.objects {
		bits := Bits{
			Name:       dependence.bits[bit(int32(node), false)],
			Underlying: dependence.bits[bit(int32(node), true)],
		}
		if bits.Name || bits.Underlying {
			dependent[object] = bits
		}
	}
	return dependent
}

// ObjectBits are object's bits in the build, read as the graph reads a unit's reference to it (resolveReference), so
// another graph can ask for an object of a package it does not hold (Counterparts): an object a unit declares has its
// own bits; one of a package the graph holds that no unit declares is read through its type, both bits set when any
// bit its type reaches is; and one of a package the graph does not hold is answered by the build's DependentObject.
// It is safe to call from one goroutine at a time, as the judgment it reads asks each object once.
func (dependence *Dependence) ObjectBits(object types.Object) Bits {
	resolved := dependence.resolved
	declared := canonical(object)
	if node, isNode := resolved.nodes[declared]; isNode {
		return Bits{Name: dependence.bits[bit(node, false)], Underlying: dependence.bits[bit(node, true)]}
	}
	if !resolved.typesPackages[declared.Pkg()] {
		return Bits{
			Name:       dependence.judging.answer(reference{object: declared}),
			Underlying: dependence.judging.answer(reference{object: declared, touchesUnderlying: true}),
		}
	}
	_, isConstant := declared.(*types.Const)
	walker := &bitWalker{seen: map[walkKey]bool{}}
	for _, through := range walker.references(declared.Type(), isConstant) {
		set := false
		if node, isNode := resolved.nodes[through.object]; isNode {
			set = dependence.bits[bit(node, through.touchesUnderlying)]
		} else if !resolved.typesPackages[through.object.Pkg()] {
			set = dependence.judging.answer(through)
		}
		if set {
			return Bits{Name: true, Underlying: true}
		}
	}
	return Bits{}
}

// Unit is one unit of the graph as a build judged it: its key, the package ID, the file a reader sees and its start
// offset in the file the host parsed, as UnitDependent takes them; its size in syntax nodes; whether its package is
// the workspace's; whether the build makes it dependent; the units it calls statically, by their index in Units; and
// the functions it calls statically of packages the graph does not hold, each once, which another graph's units may
// declare (UnitDeclaring).
type Unit struct {
	PackageId     string
	File          string
	Start         int
	Nodes         int
	Workspace     bool
	Dependent     bool
	Calls         []int
	ExternalCalls []types.Object
}

// Units are every unit of the graph, as the build judged it, in the graph's order, which their indexes name.
func (dependence *Dependence) Units() []Unit {
	units := make([]Unit, 0, len(dependence.resolved.units))
	for index := range dependence.resolved.units {
		resolved := &dependence.resolved.units[index]
		units = append(units, Unit{
			PackageId: dependence.resolved.packages[resolved.packageIndex].identifier,
			File:      resolved.record.file,
			Start:     resolved.record.start,
			Nodes:     resolved.record.nodes,
			Workspace: dependence.resolved.packages[resolved.packageIndex].workspace,
			Dependent: dependence.unitDependent[index],
			Calls:     resolved.calls,

			ExternalCalls: resolved.externalCalls,
		})
	}
	return units
}

// UnitDeclaring is the index in Units of the unit that declares object, as its generic declares it, and false when
// no unit of the graph does.
func (dependence *Dependence) UnitDeclaring(object types.Object) (int, bool) {
	node, isNode := dependence.resolved.nodes[canonical(object)]
	if !isNode || dependence.resolved.declaringUnit[node] < 0 {
		return 0, false
	}
	return dependence.resolved.declaringUnit[node], true
}

// UnitContaining is the index in Units of the unit of the package packageId whose span in file, the one a reader
// sees, holds offset, and false when none does. An object's position names its declaring unit this way in every build
// that compiles the file, since the file's bytes, and so every offset in it, are the same in each.
func (dependence *Dependence) UnitContaining(packageId string, file string, offset int) (int, bool) {
	inFile := dependence.resolved.fileUnits[fileKey{packageIdentifier: packageId, file: file}]
	// The last unit starting at or before offset is the only one that can hold it, since a file's units never overlap.
	position := sort.Search(len(inFile), func(index int) bool {
		return dependence.resolved.units[inFile[index]].record.start > offset
	}) - 1
	if position < 0 {
		return 0, false
	}
	index := inFile[position]
	if offset >= dependence.resolved.units[index].record.end {
		return 0, false
	}
	return index, true
}
