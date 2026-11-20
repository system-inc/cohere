// Package build_dependence computes which of the host's package-level declarations an extra build (linux, windows, a
// tag build) can change, so the engine can judge each declaration once and redo only the build-dependent ones per build
// (#ewgh523, #qkq4gx5). A unit is a FuncDecl, a TypeSpec or a ValueSpec, several names of one spec making one unit. It
// is build-dependent in a build when its file is not one both builds compile, or it hangs on the C side of a package
// the host's cgo tool or the build's translated (cgo_files.go), when an object it declares reaches a build-dependent
// object, or when its body uses one by a kind of use the change can reach: a type whose only change is its underlying
// type counts only where the use touches the underlying, as the engine's reads already judge (#mjdf7ah).
//
// Each named type has two bits, its name bit and its underlying bit; every other object has one, its name bit. The
// graph is built from the host's packages while their syntax and type information are live, and keeps only what a
// build's judgment reads: each unit's place and size, the objects it declares, its references as objects with the bit
// each reads, and each declared object's kind edges, so a judgment per build needs no syntax and no type maps.
//
// The algorithm and every choice in it are the scratch counter's kind-of-use variant
// (`command/scratch_dependence -kinds` on scratch-budget, ece0e3392), which found zero misses unit by unit against
// fully loaded builds of cohere's root on linux and of deeafbf4 on windows. Where it had a choice it took the larger
// set, so a doubt makes a unit dependent.
//
// A graph lives in three steps: it is built, package by package, each package's record written for the findings store
// (Graph.EncodedRecord), then sealed (Graph.Seal) before the builds are judged. Sealing resolves the graph once and
// lets go of what only resolution and encoding read, the records' units, edges and object tables among them, so the
// graph every build is judged on holds only what a judgment reads.
//
// A graph made by NewDeclarationsGraph collects declarations only: every unit's place, universe names and declared
// objects and every kind edge, and no unit's reads, calls or size, the larger part of an add's work. Its objects' bits
// are a whole graph's, since they come from the seeds and the kind edges alone, and whatever a unit's body gives, a
// verdict, the counts or a call, it refuses with a panic rather than answer wrong (#w72zxtq). Its records are
// declarations-only records, in a format of their own, which only a declarations-only graph takes (#anfjtr3).
package build_dependence

import (
	"go/ast"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Graph is the declarations of the packages checked from source, built package by package (AddPackage). Each
// package's record is collected on its caller's goroutine and published under one hold of the lock, so packages can be
// added concurrently. Against resolves the records into one graph of nodes the first time it runs after a package was
// added, and judges a build on that resolution. Once every package is added and every record the store keeps is
// written, the graph is sealed (Seal) before its builds are judged, after which nothing can be added to it and no
// record encoded from it.
type Graph struct {
	lock sync.Mutex

	// packages are the packages added, by package ID.
	packages map[string]*packageRecord

	// resolved is the graph of nodes Against judges on, nil until it is first resolved and again once a package is
	// added after, and never nil once the graph is sealed.
	resolved *resolvedGraph

	// paths finds the objectpaths every package's record names objects by (newRecord), searching each package's
	// API once for the graph, nil once the graph is sealed.
	paths *objectPaths

	// sealed says the graph was sealed (Seal): resolved for good, and holding only what a judgment reads.
	sealed bool

	// declarationsOnly says the graph collects declarations only (NewDeclarationsGraph), no unit's reads or calls. It
	// is set when the graph is made and never after, so it is read without the lock.
	declarationsOnly bool
}

// packageRecord is what the graph keeps of one package: no syntax and no type maps, only its units and the kind edges
// of the objects they declare, every object named (record.go), so a record decoded from its bytes (Graph.AddRecord)
// resolves as one collected from a live package does. path is the package's import path, imports its import map,
// path to package ID, and typeNames the names of its scope's type names, in which a method of a build-only file finds
// its receiver. unnameable counts the objects of other packages it reads that no name can carry, which a record read
// back could not find (Graph.Unnameable). Sealing the graph lets go of its imports, objects, units and edges
// (Graph.Seal), and marks it released.
type packageRecord struct {
	identifier string
	path       string
	workspace  bool
	imports    map[string]string
	typeNames  []string

	// typesPackage is the package's types, nil for a package the loader gave no types and for a record decoded from
	// its bytes, which holds no types; the graph's live object API reads it (Counterparts).
	typesPackage *types.Package

	// objects are every object the record's reads, callees and edges name, each once, in the order of their names,
	// which those name by index.
	objects []recordedObject

	units      []recordedUnit
	edges      []recordedEdge
	unnameable int

	// released says a sealed graph let go of the record's imports, objects, units and edges (Graph.Seal), so another
	// graph it was added to would hold its package with no unit and drop every read of its objects: add refuses it.
	released bool

	// declarationsOnly says the record holds no unit's reads or callees: a declarations-only graph collected it from a
	// live package (Graph.addPackage), or it was decoded from a declarations-only record's bytes. It is encoded in
	// that format (declarationsRecordFormat), and a whole graph refuses it (Graph.add).
	declarationsOnly bool
}

// unitRecord is one unit as the collector finds it in a live package, before newRecord names its objects: the file a
// reader sees it in, as its start's position names it past any //line directive, its start and end offsets in the file
// the host parsed, its size in syntax nodes, the objects it declares, and what its body or initializer reads.
type unitRecord struct {
	file  string
	start int
	end   int
	nodes int

	// declares are the package-level objects, methods, struct fields and interface methods declared anywhere in the
	// unit's header, fields of anonymous structs in a var's type or a signature among them: a unit declares them all,
	// so a unit in a file the build does not compile seeds its fields too. The counter's first trial missed exactly
	// these, 34 fields of syscall and runtime structs in seed files.
	declares []types.Object

	// receiver is a method's receiver type name, whose method set the method belongs to, nil for any other unit.
	receiver *types.TypeName

	// references are what the unit's body or initializer reads, each object once with the bit the read touches, the
	// object as its generic declares it (rule 4, see bodyWalk).
	references []reference

	// universeNames are the universe's names the unit's declaration uses, one bit each (universeNameBit), so a
	// build-only file that declares one of them can seed the units whose meaning it changes.
	universeNames uint64

	// callees are the functions the unit's body or initializer calls statically (staticCallees), which a judgment
	// reads as the units declaring them, so a caller of a function whose facts move in a build can be found.
	callees []types.Object

	// fromCgo says the unit hangs on the C side (fromCgo): it is in one of the cgo tool's outputs, a cgo file's
	// translation or the file the tool generates, or in any file of a package checked as written that has a cgo file,
	// so a build seeds it where the host's cgo tool or its own translated the package (cgo_files.go).
	fromCgo bool

	// withheld says the host's load gave the unit to the analyzers without its body or its initial value, as it does
	// of a declaration of a package checked as written that reaches cgo or holds what the faked package "C" left
	// invalid, another faked package's included (load.withholdCgoDeclarations): the host never checked what it held,
	// so the unit is a seed in every build (judgment.seedWithheld).
	withheld bool
}

// NewGraph is an empty graph.
func NewGraph() *Graph {
	return &Graph{packages: map[string]*packageRecord{}, paths: &objectPaths{}}
}

// NewDeclarationsGraph is an empty graph that collects declarations only (#w72zxtq): a package added from its live load
// (AddPackage) gives every unit's place, universe names, cgo mark and declared objects and every kind edge, as a whole
// graph's does, and no unit's reads or calls, nor its size, which only the counts read (a unit's nodes are 0). An
// object's bits come from the seeds and the fixed point over the kind edges alone (Against), so ObjectBits, NameBits
// and DependentObjects answer as a whole graph's do, and so do UnitDeclaringName and UnitContaining, which read a
// unit's place. What only a unit's body gives is refused with a panic (refuseDeclarationsOnly), never answered wrong: a
// unit's verdict (Dependence.UnitDependent, Units, CallIndex.Dependent), the counts (Dependence.Counts) and a call
// index (NewCallIndex). A record of a package it collected (EncodedRecord) is a declarations-only record, which holds
// everything it collected and says it holds no unit's reads or calls (declarationsRecordFormat).
//
// A whole record added to it (AddRecord, AddDecoded) is sound: the record only holds more than the graph reads, its
// units' reads and calls, which no answer the graph gives rests on.
func NewDeclarationsGraph() *Graph {
	return &Graph{packages: map[string]*packageRecord{}, paths: &objectPaths{}, declarationsOnly: true}
}

// AddPackage records loaded's units and their edges. It is called while loaded's syntax and TypesInfo are live, since
// the engine releases them when the package's rules end, and once it returns the graph holds no syntax node and no type
// maps of it. workspace says whether loaded belongs to the checked modules, its test variants included, or is a
// dependency, which only the counts tell apart. withheld are the positions, "file:line:column" as the load gives them
// (load.PackageWithheld.Declarations), of the declarations the load gave the analyzers without their bodies or initial
// values, each one's unit marked so (unitRecord.withheld), and every unit of the package where one names no unit's
// declaration. A package the loader gave no type information declares no units.
//
// It is safe to call concurrently for different packages: the walk runs on the caller's goroutine, in a scratch of
// its own (workerScratch), and only its publication, and a read of the graph's encoders before the walk, take the lock.
func (graph *Graph) AddPackage(loaded *packages.Package, workspace bool, withheld []string) {
	scratch, isScratch := scratches.Get().(*workerScratch)
	if !isScratch {
		// Only AddPackage puts into scratches, and only scratches; its New makes one when it holds none.
		scratch = newWorkerScratch()
	}
	graph.addPackage(loaded, workspace, withheld, scratch)
	// Given back once the record is made, and never on a panic, so the pool holds no scratch a walk left half used.
	scratch.release()
	scratches.Put(scratch)
}

// addPackage is AddPackage walking in scratch, which the caller releases before another package's walk. The graph's
// encoders are read first, so a sealed graph panics (refuseSealed) before the walk starts, and the scratch's namer
// names the package's objects by them.
func (graph *Graph) addPackage(loaded *packages.Package, workspace bool, withheld []string, scratch *workerScratch) {
	encoders := graph.encoders()
	imports := make(map[string]string, len(loaded.Imports))
	for path, imported := range loaded.Imports {
		imports[path] = imported.ID
	}
	var units []unitRecord
	var edges []kindEdge
	if loaded.TypesInfo != nil {
		collecting := scratch.collector(loaded.TypesInfo)
		collecting.declarationsOnly = graph.declarationsOnly
		if len(withheld) > 0 {
			collecting.withheld = make(map[string]bool, len(withheld))
			// Each unplaced until a unit's declaration starts at it (collector.withheld).
			for _, position := range withheld {
				collecting.withheld[position] = false
			}
		}
		cgoFiles, faked := CgoFiles(loaded), fakesCgo(loaded)
		for _, file := range loaded.Syntax {
			tokenFile := loaded.Fset.File(file.FileStart)
			collecting.fromCgo = fromCgo(loaded, cgoFiles, faked, tokenFile.Name())
			collecting.collectFile(tokenFile, file)
		}
		// A withheld position no unit's declaration starts at is one the graph cannot place, the load and the graph
		// disagreeing on where a declaration is: every unit of the package is marked withheld, the larger set, so the
		// unit the load withheld is never left without its mark.
		for _, isPlaced := range collecting.withheld {
			if !isPlaced {
				for index := range collecting.units {
					collecting.units[index].withheld = true
				}
				break
			}
		}
		if collecting.declarationsOnly {
			collecting.universeNamesFromUses(loaded.Fset)
		}
		collecting.repeatUniverseNames()
		units, edges = collecting.units, collecting.edges
	}
	var typesPackage *types.Package
	if loaded.TypesInfo != nil {
		typesPackage = loaded.Types
	}
	record := newRecord(loaded.ID, workspace, typesPackage, imports, encoders, units, edges, scratch)
	record.typesPackage = loaded.Types
	record.declarationsOnly = graph.declarationsOnly
	if record.path == "" {
		record.path = loaded.PkgPath
	}
	graph.add(record)
}

// AddRecord adds the package a record encoded (EncodedRecord), with no syntax and no types, as AddPackage added it
// from its live package, or says why the bytes hold no record. Its objects meet those of every other package by name.
func (graph *Graph) AddRecord(encoded []byte) error {
	record, decodeError := DecodeRecord(encoded)
	if decodeError != nil {
		return decodeError
	}
	graph.AddDecoded(record)
	return nil
}

// AddDecoded adds the package of a record already decoded (DecodeRecord), as AddRecord adds one from its bytes.
func (graph *Graph) AddDecoded(decoded *Record) {
	graph.add(decoded.record)
}

// encoders are the graph's objectpath encoders (Graph.paths), for a package about to be collected, read under the lock
// since Seal lets them go. A sealed graph panics (refuseSealed) before the package's walk starts.
func (graph *Graph) encoders() *objectPaths {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.refuseSealed("add a package")
	return graph.paths
}

// add publishes record under one hold of the lock, and lets the last resolution go. A sealed graph panics
// (refuseSealed), one sealed while the package's walk ran included, and so does a record another graph's Seal let go
// of (packageRecord.released), and a whole graph handed a declarations-only record (packageRecord.declarationsOnly).
func (graph *Graph) add(record *packageRecord) {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.refuseSealed("add a package")
	if record.declarationsOnly && !graph.declarationsOnly {
		// Invariant: a declarations-only record is kept under keys of its own, apart from a whole record's, and read
		// only into a declarations-only graph, so a whole graph handed one is the caller's bug, and taking it would
		// call independent every unit of the package that is dependent by its body alone.
		panic("build_dependence: a caller tried to add the declarations-only record of " + record.identifier +
			" to a whole graph (NewGraph), which would read none of its units' reads or calls and call a unit " +
			"that is dependent by its body alone independent: only a declarations-only graph " +
			"(NewDeclarationsGraph) takes it")
	}
	if record.released {
		// Invariant: each decoded Record is added to one graph, the host's or the standard library's
		// (pipeline_build_dependence.go), and the findings cache decodes its records afresh for every load, so no
		// record a sealed graph emptied reaches another; one that did is the caller's bug.
		panic("build_dependence: a caller tried to add the record of " + record.identifier + " after a graph " +
			"holding it was sealed (Graph.Seal), which let go of its units, objects and edges, so this graph would " +
			"hold the package with no unit and drop every read of its objects: a decoded Record is added to one " +
			"graph only")
	}
	graph.packages[record.identifier] = record
	graph.resolved = nil
}

// EncodedRecord is the record of the package of identifier as AddRecord reads it, and false when the graph holds no
// such package: a declarations-only record of a package a declarations-only graph collected, which only such a graph
// takes, and a whole record of any other, one a declarations-only graph took from a whole record's bytes included. A
// sealed graph panics (refuseSealed), since it let go of most of what a record holds.
func (graph *Graph) EncodedRecord(identifier string) ([]byte, bool) {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.refuseSealed("encode a record")
	record, isHeld := graph.packages[identifier]
	if !isHeld {
		return nil, false
	}
	return encodeRecord(record), true
}

// Unnameable counts, over every package added, the objects of other packages a record read that no name can carry:
// one objectpath cannot name that is no package-level object. A record read back could not find such an object, so
// persisting records rests on there being none (#6a3kjvz).
func (graph *Graph) Unnameable() int {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	count := 0
	for _, record := range graph.packages {
		count += record.unnameable
	}
	return count
}

// Seal resolves the graph, if no build was judged on it yet, and lets go of what only resolution and encoding read:
// every record's import map, object table, units and kind edges, and the graph's objectpath encoders. Resolution lets
// go of its own scaffolding, its targets, resolved paths and decoded names, as it ends. What stays of each record is
// what a judgment and the graph's other answers read: its ID, path, workspace flag, type names, types and unnameable
// count (NewCounterparts, seedFiles, seedMethodSets, count, Units, CallIndex.Workspace, Unnameable); every unit's file,
// place, size, universe names and cgo mark are the resolution's own columns, copied while it resolved. Sealing again
// does nothing.
//
// The order is safe because resolution reads the records once, all of them, and copies every node, bit, edge, call and
// unit column a judgment reads, pointing into no record, and encoding reads them only to write a record: once the
// records the store keeps are written, the graph is never resolved or encoded again. After Seal, adding a package
// (AddPackage, AddRecord, AddDecoded) or encoding a record (EncodedRecord) panics, as the caller's ordering bug it is:
// a sealed graph can neither resolve again nor encode a record, having let go of what both read. A record added from a
// decoded Record is the Record's own, so sealing lets go of that Record's too, and adding it to another graph after
// panics (packageRecord.released).
func (graph *Graph) Seal() {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	if graph.sealed {
		return
	}
	graph.resolveHeld()
	for _, record := range graph.packages {
		record.imports, record.objects, record.units, record.edges = nil, nil, nil, nil
		record.released = true
	}
	graph.paths = nil
	graph.sealed = true
}

// refuseSealed panics when the graph is sealed, saying what the caller tried. It is called under the lock, by a method
// that unlocks in a defer, so the panic leaves the lock free.
func (graph *Graph) refuseSealed(tried string) {
	if graph.sealed {
		// Invariant: the engine adds every package and writes every record the store keeps before it seals the graphs
		// (otherBuilds seals them after awaitStandardLibrary), so a call after Seal is the caller's ordering bug.
		panic("build_dependence: a caller tried to " + tried + " after the graph was sealed (Graph.Seal), which " +
			"let go of what resolving and encoding read, so the caller's order is wrong: every package is added " +
			"and every record encoded before the graph is sealed")
	}
}

// refuseDeclarationsOnly panics when declarationsOnly holds, the mode of the graph a caller asked for what only a
// unit's body gives (NewDeclarationsGraph), saying what the caller tried and what would be wrong with the answer. A
// method that calls it under the graph's lock unlocks in a defer, so the panic leaves the lock free.
func refuseDeclarationsOnly(declarationsOnly bool, tried string, wrong string) {
	if declarationsOnly {
		// Invariant: a declarations-only graph is made for its objects' bits alone, which stage 2 of the
		// declaration-grain checker joins with its typed uses (#w72zxtq), and the engine's verdicts, counts and call
		// index all come from the whole graphs it makes (pipeline_build_dependence.go), so a caller that asks one for
		// any of them is the caller's bug, and a quiet answer would be a wrong one.
		panic("build_dependence: a caller tried to " + tried + " a declarations-only graph " +
			"(NewDeclarationsGraph), which collected no unit's reads or calls, so " + wrong + ": only a whole graph " +
			"(NewGraph) answers that")
	}
}

// resolve is the graph's resolution, built if a package was added since the last one. A sealed graph's is the one
// Seal kept, never nil, since nothing can be added after.
func (graph *Graph) resolve() *resolvedGraph {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	return graph.resolveHeld()
}

// resolveHeld is resolve with the lock already held, which Seal holds while it resolves.
func (graph *Graph) resolveHeld() *resolvedGraph {
	if graph.resolved != nil {
		return graph.resolved
	}
	graph.resolved = resolveRecords(graph.packages)
	return graph.resolved
}

// canonical is an instantiated method or field as its generic declares it, the object the graph knows.
func canonical(object types.Object) types.Object {
	switch instantiated := object.(type) {
	case *types.Func:
		return instantiated.Origin()
	case *types.Var:
		return instantiated.Origin()
	}
	return object
}

// local reports whether object is declared inside a function: a local variable, constant or type, or a parameter. No
// unit declares one, so the counter read a local through its type, and a local type through what lies under it. A
// struct field or method has no parent scope and is no local, and neither is an object of the universe. go/types puts
// no blank identifier in a scope, so a blank variable, `for _, x := range` and `_ := f()` in a body among them, has no
// parent either; it is a local all the same (#6a3kjvz), read through its type as one, which is what resolution read
// for it before, as an object no unit declares. A package-level blank var is declared by its unit and never read.
func local(object types.Object) bool {
	if universal(object) {
		return false
	}
	if variable, isVariable := object.(*types.Var); isVariable && !variable.IsField() && variable.Parent() == nil {
		return true
	}
	return object.Parent() != nil && object.Parent() != object.Pkg().Scope()
}

// universal reports whether object is of no package: the universe's, `error`'s method among them, the same in every
// build.
func universal(object types.Object) bool {
	return object.Pkg() == nil
}

// countNodes is node's size: every syntax node under it, itself included.
func countNodes(node ast.Node) int {
	count := 0
	ast.Inspect(node, func(visited ast.Node) bool {
		if visited != nil {
			count++
		}
		return true
	})
	return count
}
