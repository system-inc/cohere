package build_dependence

import (
	"go/types"
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
	index, isUnit := dependence.resolved.unitStartingAt(packageId, file, offset)
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
		if object == nil {
			continue
		}
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
	if node, isNode := resolved.objectNodes[declared]; isNode {
		return Bits{Name: dependence.bits[bit(node, false)], Underlying: dependence.bits[bit(node, true)]}
	}
	if !resolved.typesPackages[declared.Pkg()] {
		return Bits{
			Name:       dependence.judging.answerObject(declared, false),
			Underlying: dependence.judging.answerObject(declared, true),
		}
	}
	_, isConstant := declared.(*types.Const)
	walker := &bitWalker{seen: map[walkKey]bool{}}
	for _, through := range walker.references(declared.Type(), isConstant) {
		set := false
		if node, isNode := resolved.objectNodes[through.object]; isNode {
			set = dependence.bits[bit(node, through.touchesUnderlying)]
		} else if !resolved.typesPackages[through.object.Pkg()] {
			set = dependence.judging.answerObject(through.object, through.touchesUnderlying)
		}
		if set {
			return Bits{Name: true, Underlying: true}
		}
	}
	return Bits{}
}

// NameBits are the bits of the object the package of path holds under local, as a record names it (#6a3kjvz), and
// whether a unit of the graph declares it; a package the graph holds one package of a path is that package, and a path
// the graph holds several of names none. It is the decoded records' ObjectBits, safe to call from one goroutine at a
// time as ObjectBits is.
func (dependence *Dependence) NameBits(path string, local string) (Bits, bool) {
	resolved := dependence.resolved
	held, isHeld := resolved.pathPackages[path]
	if !isHeld || held < 0 {
		return Bits{}, false
	}
	node, isNode := resolved.nodes[nodeKey{packageIndex: held, local: local}]
	if !isNode {
		return Bits{}, false
	}
	return Bits{Name: dependence.bits[bit(node, false)], Underlying: dependence.bits[bit(node, true)]}, true
}

// Unit is one unit of the graph as a build judged it: its key, the package ID, the file a reader sees and its start
// offset in the file the host parsed, as UnitDependent takes them; its size in syntax nodes; whether its package is
// the workspace's; whether the build makes it dependent; the units it calls statically, by their index in Units; and
// the functions it calls statically of packages the graph does not hold, each once, by name, which another graph's
// units may declare (UnitDeclaringName).
type Unit struct {
	PackageId     string
	File          string
	Start         int
	Nodes         int
	Workspace     bool
	Dependent     bool
	Calls         []int
	ExternalCalls []ObjectName
}

// ObjectName is an object as a record names it (#6a3kjvz): its package's path and its local name there, its
// objectpath, or its name for an unexported package-level object.
type ObjectName struct {
	Path  string
	Local string
}

// Units are every unit of the graph, as the build judged it, in the graph's order, which their indexes name. The
// units' calls and external calls are read out of the resolution's packed rows into one slice of each that all the
// units share, each unit's its own part of it, nil for a unit with none.
func (dependence *Dependence) Units() []Unit {
	resolved := dependence.resolved
	units := make([]Unit, 0, resolved.unitCount())
	calls := make([]int, len(resolved.calls.entries))
	for position, callee := range resolved.calls.entries {
		calls[position] = int(callee)
	}
	externalCalls := make([]ObjectName, len(resolved.externalCalls.entries))
	for position, callee := range resolved.externalCalls.entries {
		named := &resolved.externalObjects[callee]
		externalCalls[position] = ObjectName{Path: named.path, Local: named.local}
	}
	for index := range resolved.unitCount() {
		var unitCalls []int
		if first, end := resolved.calls.offsets[index], resolved.calls.offsets[index+1]; end > first {
			unitCalls = calls[first:end:end]
		}
		var unitExternalCalls []ObjectName
		if first, end := resolved.externalCalls.offsets[index], resolved.externalCalls.offsets[index+1]; end > first {
			unitExternalCalls = externalCalls[first:end:end]
		}
		units = append(units, Unit{
			PackageId: resolved.packages[resolved.unitPackage[index]].identifier,
			File:      resolved.files.names[resolved.unitFile[index]],
			Start:     int(resolved.unitStart[index]),
			Nodes:     int(resolved.unitNodes[index]),
			Workspace: resolved.packages[resolved.unitPackage[index]].workspace,
			Dependent: dependence.unitDependent[index],
			Calls:     unitCalls,

			ExternalCalls: unitExternalCalls,
		})
	}
	return units
}

// UnitDeclaringName is the index in Units of the unit that declares the object of the package of path the graph holds
// under local, and false when no unit of the graph does, or the graph holds more than one package of path.
func (dependence *Dependence) UnitDeclaringName(path string, local string) (int, bool) {
	return dependence.resolved.unitDeclaringName(path, local)
}

// UnitContaining is the index in Units of the unit of the package packageId whose span in file, the one a reader
// sees, holds offset, and false when none does. An object's position names its declaring unit this way in every build
// that compiles the file, since the file's bytes, and so every offset in it, are the same in each.
func (dependence *Dependence) UnitContaining(packageId string, file string, offset int) (int, bool) {
	return dependence.resolved.unitContaining(packageId, file, offset)
}
