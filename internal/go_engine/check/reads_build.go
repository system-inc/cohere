package check

import (
	"fmt"
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
// package's test variant at that package's path. Verdicts run on several goroutines, so its memos are written once
// and read many times without a lock (sync.Map), and only the method index, appended to, is under its lock: the
// verdicts deciding a load at once never queue on each other's hits (#b2t9xmf).
type ReadsBuild struct {
	// lock guards methodIndex and whole, and orders each package's entries in the index before it reads as indexed.
	lock sync.Mutex

	// digests are each object's closure digest, a cache.Key by types.Object, and graph the named types they are made
	// of (typeGraph).
	digests sync.Map
	graph   *typeGraph

	// paths are each object's objectpath, an objectPathResult by types.Object, and encoder the encoder that found them,
	// which keeps what it learns of each package for the next object of it, under its own lock.
	paths       sync.Map
	encoder     objectpath.Encoder
	encoderLock sync.Mutex

	// closures are each root's import closure, a []*types.Package by root, the root and every package it reaches,
	// walked once (closureOf), and methodIndex the type names of every package a walk reached with a method of each
	// name, by the name, each package's method sets computed once, when it is first indexed (indexed, a package read
	// as indexed only once its entries are in methodIndex). So no Interface or Method lookup walks a closure or a
	// method set again.
	closures    sync.Map
	methodIndex map[string][]packageMethods
	indexed     sync.Map

	// whole is the memo's twin told no build, which renders every field of every type whole, for a package with
	// assembly files (Restoring.Assembly), made the first time one is judged (wholeTwin).
	whole *ReadsBuild

	// walking and indexing, nil outside tests, are told of each root as its closure is walked and of each package as
	// it is indexed, so a test can count them.
	walking  func(*types.Package)
	indexing func(*types.Package)
}

// packageMethods are the type names of owner with a method of one name in their method sets.
type packageMethods struct {
	owner     *types.Package
	typeNames []*types.TypeName
}

// objectPathResult is an object's objectpath, and whether it has one.
type objectPathResult struct {
	path  objectpath.Path
	found bool
}

// NewReadsBuild is an empty memo of one build's closure digests, objectpaths, closures and method names.
func NewReadsBuild() *ReadsBuild {
	return &ReadsBuild{graph: newTypeGraph(), methodIndex: map[string][]packageMethods{}}
}

// wholeTwin is the memo's twin told no build, which renders every field whole, made once.
func (build *ReadsBuild) wholeTwin() *ReadsBuild {
	build.lock.Lock()
	defer build.lock.Unlock()
	if build.whole == nil {
		build.whole = NewReadsBuild()
	}
	return build.whole
}

// measureBy has the memo render another package's unexported field by its properties under sizes, the build's
// (typeGraph.measureBy), asked before the build's first verdict; a memo never told renders every field whole.
func (build *ReadsBuild) measureBy(sizes types.Sizes) {
	if sizes != nil {
		build.graph.measureBy(sizes)
	}
}

// closureDigest is object's closure digest (typeGraph.closureDigest), memoized: what an importer can observe of
// object, another package's, its own line and every named type its type reaches, each type as exportRendering writes
// it, unexported fields included, and unexported methods only where an importer sees them (importerSeesMethod), so two
// builds whose types render alike for it digest it alike, and a struct two levels down whose unexported field differs
// by target changes the digest of every object that reaches it.
// It errs wide, as exportRendering does: a digest that changes when it need not costs the reading package a recheck,
// while one that stays the same when it should change restores a stale analysis.
func (build *ReadsBuild) closureDigest(object types.Object) cache.Key {
	if known, isKnown := build.digests.Load(object); isKnown {
		digest, isDigest := known.(cache.Key)
		if !isDigest {
			// Invariant: closureDigest stores only a cache.Key.
			panic(fmt.Sprintf("check: the closure digest of %s is a %T", object.Name(), known))
		}
		return digest
	}
	digest := build.graph.closureDigest(object)
	build.digests.Store(object, digest)
	return digest
}

// pathOf is object's objectpath in its package, and whether it has one.
func (build *ReadsBuild) pathOf(object types.Object) (objectpath.Path, bool) {
	if known, isKnown := build.paths.Load(object); isKnown {
		result, isResult := known.(objectPathResult)
		if !isResult {
			// Invariant: pathOf stores only an objectPathResult.
			panic(fmt.Sprintf("check: the objectpath of %s is a %T", object.Name(), known))
		}
		return result.path, result.found
	}
	// Two verdicts may encode the same object at once; the encoder answers both alike, so either may store it.
	build.encoderLock.Lock()
	path, pathError := build.encoder.For(object)
	build.encoderLock.Unlock()
	build.paths.Store(object, objectPathResult{path: path, found: pathError == nil})
	return path, pathError == nil
}

// closureOf is root's import closure, root and every package it reaches, walked once for the build, each package of
// it indexed by its methods' names (indexPackage) before it is returned.
func (build *ReadsBuild) closureOf(root *types.Package) []*types.Package {
	if known, isKnown := build.closures.Load(root); isKnown {
		closure, isClosure := known.([]*types.Package)
		if !isClosure {
			// Invariant: closureOf stores only a []*types.Package.
			panic(fmt.Sprintf("check: the closure of %s is a %T", root.Path(), known))
		}
		return closure
	}
	if build.walking != nil {
		build.walking(root)
	}
	reached := slices.Collect(build_reads.ClosurePackages([]*types.Package{root}))
	for _, each := range reached {
		build.indexPackage(each)
	}
	// Stored once every package of it is indexed, so a verdict that reads the closure finds each in the method index.
	build.closures.Store(root, reached)
	return reached
}

// indexPackage adds the type names declaring declares to the method index, by the name of each method in their
// method sets, promoted ones included, and a pointer's for a type that is not an interface, once for the build.
func (build *ReadsBuild) indexPackage(declaring *types.Package) {
	if _, known := build.indexed.Load(declaring); known {
		return
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
	defer build.lock.Unlock()
	// Another verdict's walk may have indexed it meanwhile, and a package is indexed once. It reads as indexed only
	// once its entries are in the method index, so a verdict that skips it finds them there.
	if _, known := build.indexed.Load(declaring); known {
		return
	}
	if build.indexing != nil {
		build.indexing(declaring)
	}
	for methodName, typeNames := range byName {
		build.methodIndex[methodName] = append(build.methodIndex[methodName],
			packageMethods{owner: declaring, typeNames: typeNames})
	}
	build.indexed.Store(declaring, true)
}

// methodsNamed are the packages indexed so far with a type name with a method named name, each with those type
// names. The slice is the index's as it stands: a later package's entry is appended past its end, never into it.
func (build *ReadsBuild) methodsNamed(name string) []packageMethods {
	build.lock.Lock()
	defer build.lock.Unlock()
	return build.methodIndex[name]
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

	// Assembly is whether it has assembly files, whose frames vet's asmdecl lays out field by field: the name, offset,
	// size and kind of every field of each argument's type, another package's unexported ones too, which no property
	// stands for. Its types' reads are judged by every type they reach rendered whole, every field's type entered.
	Assembly bool
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
// A package with assembly files is judged by the memos' whole twins (Restoring.Assembly), never by another package's
// unexported field rendered by its properties.
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
	judgedHost, judgedBuild := host, build
	if restoring.Assembly {
		judgedHost, judgedBuild = host.wholeTwin(), build.wholeTwin()
	}
	judged := verdict{host: judgedHost, build: judgedBuild, closure: closure, restored: restored,
		methods: &verdictClosures{host: map[*types.Package]*gatheredClosure{}}}
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

// sortTypeNames sorts typeNames by package path and name.
//
// Mutates: typeNames is sorted in place
func sortTypeNames(typeNames []*types.TypeName) {
	slices.SortFunc(typeNames, func(left, right *types.TypeName) int {
		if order := strings.Compare(left.Pkg().Path(), right.Pkg().Path()); order != 0 {
			return order
		}
		return strings.Compare(left.Name(), right.Name())
	})
}
