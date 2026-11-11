package check

import (
	"go/types"
	"reflect"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// FactLookup is one build's facts as a verdict asks them: the fact of type factType on the object at path in the
// package at owner, or on that package itself when path is empty, and whether there is one.
type FactLookup func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool)

// ReadsBuild is what the verdicts on packages' reads compute of one build's types, the host's or an extra build's
// (linux, windows, freebsd), memoized for the build, so every package's verdict renders each object once (#b2t9xmf).
// Which packages a verdict asks of is the judged package's own (see TypesHold), since a test variant sees its
// package's test variant at that package's path. Its methods take its lock, so verdicts may run on several goroutines.
type ReadsBuild struct {
	lock sync.Mutex

	// digests are each object's closure digest, and graph the named types they are made of (typeGraph).
	digests map[types.Object]cache.Key
	graph   *typeGraph

	// paths are each object's objectpath, and encoder the encoder that found them, which keeps what it learns of each
	// package for the next object of it.
	paths   map[types.Object]objectPathResult
	encoder objectpath.Encoder

	// methodNames are each package's type names by the name of each method in their method sets (methodNamesOf).
	methodNames map[*types.Package]map[string][]*types.TypeName
}

// objectPathResult is an object's objectpath, and whether it has one.
type objectPathResult struct {
	path  objectpath.Path
	found bool
}

// NewReadsBuild is an empty memo of one build's closure digests, objectpaths and method names.
func NewReadsBuild() *ReadsBuild {
	return &ReadsBuild{
		digests:     map[types.Object]cache.Key{},
		graph:       newTypeGraph(),
		paths:       map[types.Object]objectPathResult{},
		methodNames: map[*types.Package]map[string][]*types.TypeName{},
	}
}

// closureDigest is object's closure digest (typeGraph.closureDigest), memoized: what an importer can observe of
// object, another package's, its own line and every named type its type reaches, each type as exportRendering writes
// it, unexported fields and methods included, so two builds whose types render alike for it digest it alike, and a
// struct two levels down whose unexported field differs by target changes the digest of every object that reaches it.
// It errs wide, as exportRendering does: a digest that changes when it need not costs the reading package a recheck,
// while one that stays the same when it should change restores a stale analysis.
func (build *ReadsBuild) closureDigest(object types.Object) cache.Key {
	build.lock.Lock()
	digest, known := build.digests[object]
	build.lock.Unlock()
	if known {
		return digest
	}
	digest = build.graph.closureDigest(object)
	build.lock.Lock()
	build.digests[object] = digest
	build.lock.Unlock()
	return digest
}

// pathOf is object's objectpath in its package, and whether it has one.
func (build *ReadsBuild) pathOf(object types.Object) (objectpath.Path, bool) {
	build.lock.Lock()
	defer build.lock.Unlock()
	if known, isKnown := build.paths[object]; isKnown {
		return known.path, known.found
	}
	path, pathError := build.encoder.For(object)
	build.paths[object] = objectPathResult{path: path, found: pathError == nil}
	return path, pathError == nil
}

// methodNamesOf are the type names declared package declares, by the name of each method in their method sets,
// promoted ones included, and a pointer's for a type that is not an interface, memoized for the build.
func (build *ReadsBuild) methodNamesOf(declaring *types.Package) map[string][]*types.TypeName {
	build.lock.Lock()
	known, isKnown := build.methodNames[declaring]
	build.lock.Unlock()
	if isKnown {
		return known
	}
	byName := map[string][]*types.TypeName{}
	for _, name := range declaring.Scope().Names() {
		typeName, isTypeName := declaring.Scope().Lookup(name).(*types.TypeName)
		if !isTypeName {
			continue
		}
		setOf := typeName.Type()
		if !types.IsInterface(setOf) {
			setOf = types.NewPointer(setOf)
		}
		methods := types.NewMethodSet(setOf)
		for index := range methods.Len() {
			methodName := methods.At(index).Obj().Name()
			byName[methodName] = append(byName[methodName], typeName)
		}
	}
	build.lock.Lock()
	build.methodNames[declaring] = byName
	build.lock.Unlock()
	return byName
}

// Restoring is one package of the host's run whose reads a verdict judges in another build.
type Restoring struct {
	// Path is the package's path, whose own objects a read never asks of the other build: its own inputs are the
	// host's there, which the portable digest compares (cache.Keyed.Portable).
	Path string

	// Imports are the paths of the packages it imports, the roots of its import closure in the other build, the same
	// as the host's while its own inputs are.
	Imports []string

	// Reads are its reads on the host's run.
	Reads *build_reads.Reads
}

// deprecatedFactType is the deprecated analyzer's fact, which its result gathers for SA1019 from every fact of the
// import closure, so a Deprecated lookup is that fact asked of the build.
var deprecatedFactType = reflect.TypeFor[*deprecated.IsDeprecated]()

// TypesHold reports whether what restoring read of other packages' types on the host's run, in host, reads the same in
// build, whose packages it reaches are closure, by path, restored those of them build restored from the host's run,
// and when it does not, the first read that differs, named by
// its row: T for an object or instance its type check reached, L for a rule's lookup of the types, and never or
// unknown for reads that cannot hold at all. It is the load's half of the verdict, decided once the package's imports
// are done in build and before anything of the package is read there (load.Restored.Holds); FactsHold is check's. A
// package whose own inputs are the host's and whose reads hold may take the host's results in build (#b2t9xmf).
//
// Every read fails closed: an object with no objectpath, a package the build lacks, and a lookup of a kind it does not
// know each fail it.
func TypesHold(
	restoring Restoring,
	closure map[string]*types.Package,
	restored map[*types.Package]bool,
	host *ReadsBuild,
	build *ReadsBuild,
) (bool, string) {
	if restorable, why := restoring.Reads.Restorable(); !restorable {
		if restoring.Reads.Never() != "" {
			return false, "never: " + why
		}
		return false, "unknown: " + why
	}
	judged := verdict{host: host, build: build, closure: closure, restored: restored}
	for _, object := range restoring.Reads.Objects() {
		if holds, why := judged.objectHolds(object); !holds {
			return false, "T " + why
		}
	}
	for _, instance := range restoring.Reads.Instances() {
		if holds, why := judged.instanceHolds(instance, restoring.Path); !holds {
			return false, "T " + why
		}
	}
	for _, lookup := range restoring.Reads.Lookups() {
		if lookup.Query == build_reads.QueryDeprecated {
			continue
		}
		if holds, why := judged.lookupHolds(lookup, restoring); !holds {
			return false, "L " + why
		}
	}
	return true, ""
}

// FactsHold reports whether the fact reads restoring made on the host's run of the types among factTypes, and its
// deprecation lookups when the deprecated fact is among them, read the same in build, whose facts fact gives as one
// action on the package inherited them, and when they do not, the first that differs: F for a fact, D for a
// deprecation. It is check's half of the verdict, decided as each action on the package is restored, once every
// action it inherits from has finished; an action asks only the fact types its analyzer learns, so the caller tells
// which read types every action asked.
func FactsHold(
	restoring Restoring,
	closure map[string]*types.Package,
	host *ReadsBuild,
	build *ReadsBuild,
	fact FactLookup,
	factTypes map[reflect.Type]bool,
) (bool, string) {
	judged := verdict{host: host, build: build, closure: closure, fact: fact}
	for _, read := range restoring.Reads.Facts() {
		if !factTypes[read.FactType] {
			continue
		}
		if holds, why := judged.factHolds(read); !holds {
			return false, "F " + why
		}
	}
	if !factTypes[deprecatedFactType] {
		return true, ""
	}
	for _, lookup := range restoring.Reads.Lookups() {
		if lookup.Query != build_reads.QueryDeprecated {
			continue
		}
		if holds, why := judged.deprecationHolds(lookup); !holds {
			return false, "D " + why
		}
	}
	return true, ""
}

// ReadFactTypes are the fact types restoring's fact reads asked, and the deprecated fact's when it looked up a
// deprecation, every type FactsHold must be asked of before its fact reads are all decided.
func ReadFactTypes(restoring Restoring) map[reflect.Type]bool {
	asked := map[reflect.Type]bool{}
	for _, read := range restoring.Reads.Facts() {
		asked[read.FactType] = true
	}
	for _, lookup := range restoring.Reads.Lookups() {
		if lookup.Query == build_reads.QueryDeprecated {
			asked[deprecatedFactType] = true
		}
	}
	return asked
}

// objectName is object as a reason names it, its package's path and its name.
func objectName(object types.Object) string {
	if object.Pkg() == nil {
		return object.Name()
	}
	return object.Pkg().Path() + "." + object.Name()
}

// methodCandidates are the type names of the import closure of roots with a method of a lookup's name in their method
// sets, as namesOf gives each package's, sorted by package path and name: every type whose method an Interface or
// Method lookup of the name may match, by whichever measure the asking rule takes.
func methodCandidates(roots []*types.Package, namesOf func(*types.Package) []*types.TypeName) []*types.TypeName {
	var candidates []*types.TypeName
	for reached := range build_reads.ClosurePackages(roots) {
		candidates = append(candidates, namesOf(reached)...)
	}
	slices.SortFunc(candidates, func(left, right *types.TypeName) int {
		if order := strings.Compare(left.Pkg().Path(), right.Pkg().Path()); order != 0 {
			return order
		}
		return strings.Compare(left.Name(), right.Name())
	})
	return candidates
}
