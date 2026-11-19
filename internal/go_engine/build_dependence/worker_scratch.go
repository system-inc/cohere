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
// added (release), and the pool lets go of what it holds within two collections, so an idle worker keeps nothing for
// long.
//
// No bound is put on what a scratch keeps for the next package: each keeps the room the largest package it met grew,
// and the pool lets all of it go within two collections, so the room is held only while packages are being added. A
// bound of 16 K entries a buffer, measured at cohere's root, bought 20 MB of the add's 411 MB at 16 workers and 38 MB
// of 561 at 64, and left the live heap with the graphs held the same, since what the add still allocates is each
// worker's first growth to its largest package (#86qvr6b). The CPU was measured too: the add took 3.86 s at 16 workers
// with the bound and 3.89 s without, so clearing a table a large package grew, for every small package after it,
// costs nothing visible.
var scratches = sync.Pool{New: func() any { return newWorkerScratch() }}

// newWorkerScratch is a scratch with every map made and nothing in it, its walk's parts pointing at each other.
func newWorkerScratch() *workerScratch {
	scratch := &workerScratch{}
	scratch.walker = bitWalker{seen: map[walkKey]bool{}}
	scratch.bodies = bodyWalk{walker: &scratch.walker, typeReferences: map[walkKey][]reference{},
		collected: map[reference]bool{}}
	scratch.collecting = collector{edgeSeen: map[kindEdge]bool{}, walker: &scratch.walker, bodies: &scratch.bodies,
		calleeSeen: map[types.Object]bool{}, passedSeen: map[types.Type]bool{}}
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
func (scratch *workerScratch) release() {
	clear(scratch.walker.seen)
	scratch.walker.collected = emptied(scratch.walker.collected)
	scratch.walker.entering = false

	scratch.bodies.typesInformation = nil
	clear(scratch.bodies.typeReferences)
	scratch.bodies.typeReferenceStore = emptied(scratch.bodies.typeReferenceStore)
	clear(scratch.bodies.collected)
	scratch.bodies.referenceStore = emptied(scratch.bodies.referenceStore)

	scratch.collecting.typesInformation = nil
	scratch.collecting.declarationsOnly = false
	scratch.collecting.units = emptied(scratch.collecting.units)
	scratch.collecting.edges = emptied(scratch.collecting.edges)
	clear(scratch.collecting.edgeSeen)
	clear(scratch.collecting.calleeSeen)
	scratch.collecting.calleeStore = emptied(scratch.collecting.calleeStore)
	scratch.collecting.declareStore = emptied(scratch.collecting.declareStore)
	scratch.collecting.declaresStart = 0
	scratch.collecting.owners = emptied(scratch.collecting.owners)
	scratch.collecting.unitWalk = unitVisitor{}
	scratch.collecting.membersWalk = membersVisitor{}
	scratch.collecting.usesWalk = usesVisitor{}
	scratch.collecting.passedTypes = emptied(scratch.collecting.passedTypes)
	clear(scratch.collecting.passedSeen)
	scratch.collecting.initializerWalk = initializerVisitor{}

	scratch.naming.typesPackage = nil
	scratch.naming.encoder = nil
	scratch.naming.record = nil
	scratch.naming.unnameable = 0
	clear(scratch.naming.declared)
	clear(scratch.naming.names)
	clear(scratch.naming.throughs)
	clear(scratch.naming.indexes)
	scratch.naming.objects = emptied(scratch.naming.objects)
	scratch.naming.edges = emptied(scratch.naming.edges)
	scratch.naming.callees = emptied(scratch.naming.callees)
	scratch.naming.byName = emptied(scratch.naming.byName)
	scratch.naming.renumbered = emptied(scratch.naming.renumbered)
}

// emptied is held cut to nothing for the next package, its room kept and every element up to its capacity zeroed, so
// it points at nothing of the last.
func emptied[Element any](held []Element) []Element {
	clear(held[:cap(held)])
	return held[:0]
}
