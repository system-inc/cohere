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
package build_dependence

import (
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Graph is the declarations of the packages checked from source, built package by package (AddPackage). Each
// package's record is collected on its caller's goroutine and published under one hold of the lock, so packages can be
// added concurrently. Against resolves the records into one graph of nodes the first time it runs after a package was
// added, and judges a build on that resolution.
type Graph struct {
	lock sync.Mutex

	// packages are the packages added, by package ID.
	packages map[string]*packageRecord

	// resolved is the graph of nodes Against judges on, nil until it is first resolved and again once a package is
	// added after.
	resolved *resolvedGraph
}

// packageRecord is what the graph keeps of one package: no syntax and no type maps, only its units and the kind edges
// of the objects they declare.
type packageRecord struct {
	identifier string
	workspace  bool

	// typesPackage is the package's types, in whose scope a method of a build-only file finds its receiver's type
	// name. It is nil for a package the loader gave no types.
	typesPackage *types.Package

	units []unitRecord
	edges []kindEdge
}

// unitRecord is one unit: the file a reader sees it in, as its start's position names it past any //line directive,
// its start and end offsets in the file the host parsed, its size in syntax nodes, the objects it declares, and what
// its body or initializer reads.
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
}

// NewGraph is an empty graph.
func NewGraph() *Graph {
	return &Graph{packages: map[string]*packageRecord{}}
}

// AddPackage records loaded's units and their edges. It is called while loaded's syntax and TypesInfo are live, since
// the engine releases them when the package's rules end, and once it returns the graph holds no syntax node and no type
// maps of it. workspace says whether loaded belongs to the checked modules, its test variants included, or is a
// dependency, which only the counts tell apart. A package the loader gave no type information declares no units.
//
// It is safe to call concurrently for different packages: the walk runs on the caller's goroutine, and only its
// publication takes the lock.
func (graph *Graph) AddPackage(loaded *packages.Package, workspace bool) {
	record := &packageRecord{identifier: loaded.ID, workspace: workspace, typesPackage: loaded.Types}
	if loaded.TypesInfo != nil {
		collecting := newCollector(loaded.TypesInfo)
		for _, file := range loaded.Syntax {
			collecting.collectFile(loaded.Fset.File(file.FileStart), file)
		}
		record.units = collecting.units
		record.edges = collecting.edges
	}

	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.packages[record.identifier] = record
	graph.resolved = nil
}

// resolve is the graph's resolution, built if a package was added since the last one.
func (graph *Graph) resolve() *resolvedGraph {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	if graph.resolved != nil {
		return graph.resolved
	}
	resolved := &resolvedGraph{
		packageIndex:  map[string]int{},
		typesPackages: map[*types.Package]bool{},
		nodes:         map[types.Object]int32{},
		unitIndex:     map[unitKey]int{},
	}
	for _, identifier := range slices.Sorted(maps.Keys(graph.packages)) {
		record := graph.packages[identifier]
		resolved.packageIndex[identifier] = len(resolved.packages)
		resolved.packages = append(resolved.packages, record)
		if record.typesPackage != nil {
			resolved.typesPackages[record.typesPackage] = true
		}
	}
	resolved.resolveUnits()
	resolved.resolveEdges()
	walker := &bitWalker{seen: map[walkKey]bool{}}
	for index := range resolved.units {
		for _, read := range resolved.units[index].record.references {
			resolved.resolveReference(&resolved.units[index], read, walker)
		}
	}
	graph.resolved = resolved
	return resolved
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
// struct field or method has no parent scope and is no local, and neither is an object of the universe.
func local(object types.Object) bool {
	return !universal(object) && object.Parent() != nil && object.Parent() != object.Pkg().Scope()
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
