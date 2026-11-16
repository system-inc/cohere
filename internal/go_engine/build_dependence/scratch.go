package build_dependence

import (
	"go/types"
	"sync"
)

// workerScratch is the walk and the namer one worker adds packages with (Graph.AddPackage), their maps and slices kept
// from one package to the next, so a worker that adds many packages grows them once rather than once a package: block
// 1's add phase allocated 879 MB in 4.0 M objects at cohere's root, of which the records keep 130 MB, and beside the
// lint every allocation lands on fresh spans at its peak heap (#86qvr6b). AddPackage runs on several goroutines at
// once, so each takes a scratch of its own from scratches and gives it back once its record is made.
//
// No record holds scratch memory: newRecord copies what it reads of the units, their declares, references and callees,
// into the record's own slices, and the expansions reads keep are made fresh for each object (namer.through), so the
// next package's walk can write over every slice here.
type workerScratch struct {
	walker     bitWalker
	bodies     bodyWalk
	collecting collector
	naming     namer
}

// scratches are the workers' scratches between packages. A scratch in the pool holds nothing of the package it last
// added (release), and the pool lets go of what it holds at a collection, so an idle worker keeps nothing for long.
var scratches = sync.Pool{New: func() any { return newWorkerScratch() }}

// scratchHeld bounds what a scratch keeps for the next package: a map that held more entries than this, or a slice of a
// larger capacity, is let go rather than cleared, so a worker never holds one huge package's maps from then on, nor
// clears a table that large for every small package after it. At 16 K entries the widest of them, edgeSeen's, holds a
// few megabytes.
const scratchHeld = 1 << 14

// newWorkerScratch is a scratch with every map made and nothing in it, its walk's parts pointing at each other.
func newWorkerScratch() *workerScratch {
	scratch := &workerScratch{}
	scratch.walker = bitWalker{seen: map[walkKey]bool{}}
	scratch.bodies = bodyWalk{walker: &scratch.walker, typeReferences: map[walkKey][]reference{},
		collected: map[reference]bool{}}
	scratch.collecting = collector{edgeSeen: map[kindEdge]bool{}, walker: &scratch.walker, bodies: &scratch.bodies,
		calleeSeen: map[types.Object]bool{}}
	scratch.naming = namer{declared: map[types.Object]string{}, names: map[types.Object]string{},
		throughs: map[types.Object][]recordedBit{}, indexes: map[types.Object]int32{}}
	return scratch
}

// collector is scratch's walk of the declarations of the package typesInformation describes, every map and slice of it
// empty.
func (scratch *workerScratch) collector(typesInformation *types.Info) *collector {
	scratch.collecting.typesInformation = typesInformation
	scratch.bodies.typesInformation = typesInformation
	return &scratch.collecting
}

// namer is scratch's namer of the objects of typesPackage into record, finding objectpaths by encoder, every map of it
// empty.
func (scratch *workerScratch) namer(typesPackage *types.Package, encoder *objectPaths, record *packageRecord) *namer {
	scratch.naming.typesPackage = typesPackage
	scratch.naming.encoder = encoder
	scratch.naming.record = record
	return &scratch.naming
}

// release readies scratch for the next package: every map emptied and every slice cut to nothing with its elements
// zeroed, and the type information, encoder and record it pointed at dropped, so a scratch waiting in the pool holds
// no object of the package it last added, whose syntax and types the engine lets go once the package's rules end.
//
// Mutates: scratch
func (scratch *workerScratch) release() {
	scratch.walker.seen = clearedMap(scratch.walker.seen, scratch.walker.seenMost)
	scratch.walker.seenMost = 0
	scratch.walker.collected = clearedSlice(scratch.walker.collected)

	scratch.bodies.typesInformation = nil
	scratch.bodies.typeReferences = clearedMap(scratch.bodies.typeReferences, len(scratch.bodies.typeReferences))
	scratch.bodies.typeReferenceStore = clearedSlice(scratch.bodies.typeReferenceStore)
	scratch.bodies.collected = clearedMap(scratch.bodies.collected, scratch.bodies.collectedMost)
	scratch.bodies.collectedMost = 0
	scratch.bodies.referenceStore = clearedSlice(scratch.bodies.referenceStore)

	scratch.collecting.typesInformation = nil
	scratch.collecting.units = clearedSlice(scratch.collecting.units)
	scratch.collecting.edges = clearedSlice(scratch.collecting.edges)
	scratch.collecting.edgeSeen = clearedMap(scratch.collecting.edgeSeen, len(scratch.collecting.edgeSeen))
	scratch.collecting.calleeSeen = clearedMap(scratch.collecting.calleeSeen, scratch.collecting.calleeSeenMost)
	scratch.collecting.calleeSeenMost = 0
	scratch.collecting.calleeStore = clearedSlice(scratch.collecting.calleeStore)
	scratch.collecting.unitWalk = unitVisitor{}
	scratch.collecting.usesWalk = usesVisitor{}

	scratch.naming.typesPackage = nil
	scratch.naming.encoder = nil
	scratch.naming.record = nil
	scratch.naming.unnameable = 0
	scratch.naming.declared = clearedMap(scratch.naming.declared, len(scratch.naming.declared))
	scratch.naming.names = clearedMap(scratch.naming.names, len(scratch.naming.names))
	scratch.naming.throughs = clearedMap(scratch.naming.throughs, len(scratch.naming.throughs))
	scratch.naming.indexes = clearedMap(scratch.naming.indexes, len(scratch.naming.indexes))
}

// clearedMap is held emptied for the next package, or a new map in its place when it once held more than scratchHeld
// entries, most being the most it held, since a map never gives back the room it grew to.
func clearedMap[Key comparable, Value any](held map[Key]Value, most int) map[Key]Value {
	if most > scratchHeld {
		return map[Key]Value{}
	}
	clear(held)
	return held
}

// clearedSlice is held cut to nothing for the next package, its every element up to its capacity zeroed so it points
// at nothing of the last, or nil when its capacity is past scratchHeld.
func clearedSlice[Element any](held []Element) []Element {
	if cap(held) > scratchHeld {
		return nil
	}
	clear(held[:cap(held)])
	return held[:0]
}
