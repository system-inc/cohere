// Package build_dependence computes which of the host's package-level declarations an extra build (linux, windows, a
// tag build) can change, so the engine can judge each declaration once and redo only the build-dependent ones per
// build (#ewgh523, #qkq4gx5). A unit is a FuncDecl, a TypeSpec or a ValueSpec, several names of one spec making one
// unit. It is build-dependent in a build when its file is not one both builds compile, when an object it declares
// reaches a build-dependent object, or when its body uses one by a kind of use the change can reach: a type whose only
// change is its underlying type counts only where the use touches the underlying, as the engine's reads already judge
// (#mjdf7ah).
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
}

// packageRecord is what the graph keeps of one package: no syntax and no type maps, only its units and the kind edges
// of the objects they declare, every object named (record.go), so a record decoded from its bytes (Graph.AddRecord)
// resolves as one collected from a live package does. path is the package's import path, imports its import map,
// path to package ID, and typeNames the names of its scope's type names, in which a method of a build-only file finds
// its receiver. unnameable counts the objects of other packages it reads that no name can carry, which a record read
// back could not find (Graph.Unnameable). Sealing the graph lets go of its imports, objects, units and edges
// (Graph.Seal).
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
}

// NewGraph is an empty graph.
func NewGraph() *Graph {
	return &Graph{packages: map[string]*packageRecord{}, paths: &objectPaths{}}
}

// AddPackage records loaded's units and their edges. It is called while loaded's syntax and TypesInfo are live, since
// the engine releases them when the package's rules end, and once it returns the graph holds no syntax node and no type
// maps of it. workspace says whether loaded belongs to the checked modules, its test variants included, or is a
// dependency, which only the counts tell apart. A package the loader gave no type information declares no units.
//
// It is safe to call concurrently for different packages: the walk runs on the caller's goroutine, and only its
// publication, and a read of the graph's encoders before the walk, take the lock.
func (graph *Graph) AddPackage(loaded *packages.Package, workspace bool) {
	encoders := graph.encoders()
	imports := make(map[string]string, len(loaded.Imports))
	for path, imported := range loaded.Imports {
		imports[path] = imported.ID
	}
	var units []unitRecord
	var edges []kindEdge
	if loaded.TypesInfo != nil {
		collecting := newCollector(loaded.TypesInfo)
		for _, file := range loaded.Syntax {
			collecting.collectFile(loaded.Fset.File(file.FileStart), file)
		}
		units, edges = collecting.units, collecting.edges
	}
	var typesPackage *types.Package
	if loaded.TypesInfo != nil {
		typesPackage = loaded.Types
	}
	record := newRecord(loaded.ID, workspace, typesPackage, imports, encoders, units, edges)
	record.typesPackage = loaded.Types
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
// (refuseSealed), one sealed while the package's walk ran included.
func (graph *Graph) add(record *packageRecord) {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.refuseSealed("add a package")
	graph.packages[record.identifier] = record
	graph.resolved = nil
}

// EncodedRecord is the record of the package of identifier as AddRecord reads it, and false when the graph holds no
// such package. A sealed graph panics (refuseSealed), since it let go of most of what a record holds.
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
// place, size and universe names are the resolution's own columns, copied while it resolved. Sealing again does
// nothing.
//
// The order is safe because resolution reads the records once, all of them, and copies every node, bit, edge, call and
// unit column a judgment reads, pointing into no record, and encoding reads them only to write a record: once the
// records the store keeps are written, the graph is never resolved or encoded again. After Seal, adding a package
// (AddPackage, AddRecord, AddDecoded) or encoding a record (EncodedRecord) panics, as the caller's ordering bug it is:
// a sealed graph can neither resolve again nor encode a record, having let go of what both read. A record added from a
// decoded Record is the Record's own, so sealing lets go of that Record's too, and it can be added to no other graph
// after.
func (graph *Graph) Seal() {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	if graph.sealed {
		return
	}
	graph.resolveHeld()
	for _, record := range graph.packages {
		record.imports, record.objects, record.units, record.edges = nil, nil, nil, nil
	}
	graph.paths = nil
	graph.sealed = true
}

// refuseSealed panics when the graph is sealed, saying what the caller tried. It is called under the lock, by a method
// that unlocks in a defer, so the panic leaves the lock free.
func (graph *Graph) refuseSealed(tried string) {
	if graph.sealed {
		panic("build_dependence: a caller tried to " + tried + " after the graph was sealed (Graph.Seal), which " +
			"let go of what resolving and encoding read, so the caller's order is wrong: every package is added " +
			"and every record encoded before the graph is sealed")
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
