package build_dependence

import (
	"go/types"
	"slices"
)

// namer names one live package's objects for its record: the package, its declared objects' local names, the graph's
// encoders that find objectpaths (Graph.paths), each object's name and expansion once, each named object's index in
// the record's table (packageRecord.objects), the record, and how many objects of another package it met that no name
// can carry (Graph.Unnameable). Its maps and slices are a worker's scratch, kept from one package to the next
// (workerScratch); the expansions throughs holds are each made fresh, since the record's reads keep them.
type namer struct {
	typesPackage *types.Package
	declared     map[types.Object]string
	encoder      *objectPaths
	names        map[types.Object]string
	throughs     map[types.Object][]recordedBit
	indexes      map[types.Object]int32
	record       *packageRecord
	unnameable   int

	// objects is the record's table as objects are met, edges its edges and callees the current unit's callees, each
	// grown here and copied into the record at its size (order, newRecord), so the record keeps no slice grown by
	// doubling and no grown slice is left behind for each package; byName and renumbered are order's permutations.
	objects    []recordedObject
	edges      []recordedEdge
	callees    []int32
	byName     []int32
	renumbered []int32
}

// nameOf is object's local name in its own package, empty when it has none: one its package's units declare by its
// declared name, any other by its objectpath, or for a package-level object objectpath refuses, its name.
func (naming *namer) nameOf(object types.Object) string {
	if local, isDeclared := naming.declared[object]; isDeclared {
		return local
	}
	if local, isNamed := naming.names[object]; isNamed {
		return local
	}
	local := localName(naming.encoder, object)
	naming.names[object] = local
	return local
}

// localName is object's local name by encoder, as no unit's declaring names it: a package-level object's name, which
// is its objectpath where it has one, and any other object's objectpath, empty for none. A package-level object is
// named without the encoder, whose search of a package's API is the costly part of naming.
func localName(encoder *objectPaths, object types.Object) string {
	if packageLevel(object) {
		return object.Name()
	}
	if path, pathError := encoder.For(object); pathError == nil {
		return string(path)
	}
	return ""
}

// own reports whether object is of the package being named.
func (naming *namer) own(object types.Object) bool {
	return object.Pkg() == naming.typesPackage
}

// recorded is object's index in the record's table, added there on its first use, and whether it is named. An object
// of another package with no name is counted (unnameable), since a record read back could not find it.
func (naming *namer) recorded(object types.Object) (int32, bool) {
	if index, isKnown := naming.indexes[object]; isKnown {
		return index, true
	}
	local := naming.nameOf(object)
	if local == "" {
		if !naming.own(object) {
			naming.unnameable++
		}
		return -1, false
	}
	index := int32(len(naming.objects))
	naming.indexes[object] = index
	naming.objects = append(naming.objects, recordedObject{path: object.Pkg().Path(), local: local, object: object})
	return index, true
}

// order gives the record its table, the namer's in the order of its objects' names, every index into it renumbered,
// and each unit's reads in their order (compareReads): the body walk collects a unit's references in a map, and the
// table fills in the order objects are met, so without it a record of the same package would be other bytes in another
// run. Every expansion a read holds is one of throughs', each renumbered once.
func (naming *namer) order() {
	record := naming.record
	naming.byName = resized(naming.byName, len(naming.objects))
	for index := range naming.byName {
		naming.byName[index] = int32(index)
	}
	slices.SortStableFunc(naming.byName, func(left int32, right int32) int {
		return compareObjects(naming.objects[left], naming.objects[right])
	})
	// byName is a permutation, so every index of renumbered is written.
	naming.renumbered = resized(naming.renumbered, len(naming.byName))
	renumbered := naming.renumbered
	record.objects = make([]recordedObject, len(naming.byName))
	for position, index := range naming.byName {
		renumbered[index] = int32(position)
		record.objects[position] = naming.objects[index]
	}
	for _, bits := range naming.throughs {
		for index := range bits {
			bits[index].object = renumbered[bits[index].object]
		}
	}
	for unitIndex := range record.units {
		unit := &record.units[unitIndex]
		for index := range unit.reads {
			if unit.reads[index].target >= 0 {
				unit.reads[index].target = renumbered[unit.reads[index].target]
			}
		}
		for index := range unit.callees {
			unit.callees[index] = renumbered[unit.callees[index]]
		}
		slices.SortFunc(unit.reads, compareReads)
	}
	for index := range record.edges {
		record.edges[index].to = renumbered[record.edges[index].to]
	}
}

// resized is held at length, its room reused when it has enough, its elements left for the caller to write.
func resized(held []int32, length int) []int32 {
	if cap(held) < length {
		return make([]int32, length)
	}
	return held[:length]
}

// through is what reading object reaches through its type, a constant's in full, as resolution reads an object no unit
// declares: each bit named, an object of the package's own that no unit declares left out, as resolution drops it.
func (naming *namer) through(object types.Object, walker *bitWalker) []recordedBit {
	if bits, isKnown := naming.throughs[object]; isKnown {
		return bits
	}
	_, isConstant := object.(*types.Const)
	var bits []recordedBit
	// Naming an object walks no type, so the walker's own buffer serves; bits is made fresh, the record keeping it.
	for _, reached := range walker.borrowedReferences(object.Type(), isConstant) {
		if naming.own(reached.object) {
			if _, isDeclared := naming.declared[reached.object]; !isDeclared {
				continue
			}
		}
		index, isNamed := naming.recorded(reached.object)
		if !isNamed {
			continue
		}
		bits = append(bits, recordedBit{object: index, underlying: reached.touchesUnderlying})
	}
	naming.throughs[object] = bits
	return bits
}

// read is reference as a unit's record holds it (recordedRead). One with neither a target nor anything reached through
// its type reads no bit, and its unit's record leaves it out.
func (naming *namer) read(reference reference, walker *bitWalker) recordedRead {
	read := recordedRead{target: -1, underlying: reference.touchesUnderlying}
	if naming.own(reference.object) {
		if _, isDeclared := naming.declared[reference.object]; isDeclared {
			read.target, _ = naming.recorded(reference.object)
			return read
		}
		read.through = naming.through(reference.object, walker)
		return read
	}
	hasTarget := false
	read.target, hasTarget = naming.recorded(reference.object)
	if !hasTarget || !declaresEvery(reference.object) {
		read.through = naming.through(reference.object, walker)
	}
	return read
}
