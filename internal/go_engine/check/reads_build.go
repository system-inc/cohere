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
	// walked once (closureOf), indexedRoots the roots whose whole closure is in the method index (indexedClosureOf),
	// and methodIndex the type names of every package indexed with a method of each name, as the rules index them
	// (indexPackage), by the name, each package's methods read once, when it is first indexed (indexed, a package read
	// as indexed only once its entries are in methodIndex). So no Interface or Method lookup walks a closure or a
	// method set again.
	closures     sync.Map
	indexedRoots sync.Map
	methodIndex  map[string][]packageMethods
	indexed      sync.Map

	// whole is the memo's twin told no build, which renders every field of every type whole, for a package with
	// assembly files (Restoring.Assembly), made the first time one is judged (wholeTwin).
	whole *ReadsBuild

	// uses are each object's digest as a use of each kind but the whole judges it, a cache.Key by useKey (useDigest).
	uses sync.Map

	// candidates are each type name's candidate for a Method or Interface lookup of a query and name, a
	// memoizedCandidate by candidateKey (candidate).
	candidates sync.Map

	// dependences are each package's dependence set, a DependenceSet by the package's *build_reads.Reads, asked of
	// the host's memo alone, which every build shares (Dependence), and reaches the packages each type name's closure
	// reaches, which the sets are gathered from (typeReach).
	dependences sync.Map
	reaches     typeReach

	// walking and indexing, nil outside tests, are told of each root as its closure is walked and of each package as
	// it is indexed, so a test can count them.
	walking  func(*types.Package)
	indexing func(*types.Package)
}

// packageMethods are the type names of owner with a method of one name, as indexPackage indexes them.
type packageMethods struct {
	owner     *types.Package
	typeNames []*types.TypeName
}

// useKey is one object judged as one kind of use.
type useKey struct {
	object types.Object
	kind   build_reads.UseKind
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

// measuredSizes are the word size and the maximum alignment of the sizes the memo measures another package's unexported
// fields by (measureBy), and false for a memo never told its build, which renders every field whole. The maximum
// alignment is int64's, which the gc compiler's sizes, the only ones a load gives, cap at it, and which no target
// aligns past 8.
func (build *ReadsBuild) measuredSizes() (int64, int64, bool) {
	sizes := build.graph.measuredSizes()
	if sizes == nil {
		return 0, 0, false
	}
	return sizes.Sizeof(types.Typ[types.Uintptr]), sizes.Alignof(types.Typ[types.Int64]), true
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

// useDigest is object's digest as a use of kind judges it (#mjdf7ah), memoized: a whole use's closure digest
// (closureDigest), and for every other kind the lines of the object that kind reads (useLines), so two builds digest a
// use alike exactly when what the use read of the object renders alike in both.
func (build *ReadsBuild) useDigest(object types.Object, kind build_reads.UseKind) cache.Key {
	if kind == build_reads.UseWhole {
		return build.closureDigest(object)
	}
	key := useKey{object: object, kind: kind}
	if known, isKnown := build.uses.Load(key); isKnown {
		digest, isDigest := known.(cache.Key)
		if !isDigest {
			// Invariant: useDigest stores only a cache.Key.
			panic(fmt.Sprintf("check: the use digest of %s is a %T", object.Name(), known))
		}
		return digest
	}
	lines := build.useLines(object, kind)
	parts := make([][]byte, 0, len(lines)+1)
	parts = append(parts, []byte(kind))
	for _, line := range lines {
		parts = append(parts, []byte(line))
	}
	digest := cache.DigestOf(parts...)
	build.uses.Store(key, digest)
	return digest
}

// useLines are the lines of object a use of kind reads (build_reads.UseKind):
//   - a member, its own line (ownRendering), and the line of each field of an unnamed struct its own type holds that
//     the build renders by its properties, as its closure digest's own part writes them;
//   - a type name named, its path, name, type parameters and kind, and for an alias the type it names;
//   - its underlying type, that and the underlying type written, every named type in it by name;
//   - its method set, that and every method of the type and of a pointer to it an importer can see (importerSees), by
//     name, index, indirection and own line.
//
// A kind about a type, asked of an object that is no type name, reads the object's closure, which reads the most.
func (build *ReadsBuild) useLines(object types.Object, kind build_reads.UseKind) []string {
	if kind == build_reads.UseMember {
		step := directTypeNames(rootTypes(object), build.graph.sizesForWalk())
		return append([]string{ownRendering(object)}, step.observed...)
	}
	typeName, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		closure := build.closureDigest(object)
		return []string{"closure " + string(closure[:])}
	}
	lines := []string{typeNameLine(typeName)}
	switch kind {
	case build_reads.UseUnderlying:
		lines = append(lines, "underlying "+writtenType(typeName.Type().Underlying()))
	case build_reads.UseMethodSet:
		lines = append(lines, methodSetLines(typeName, build.graph.importerSees)...)
	}
	return lines
}

// typeNameLine is a type name as a use of its name reads it: its package's path, its name, its type parameters, its
// kind, and for an alias the type it names, written.
func typeNameLine(typeName *types.TypeName) string {
	line := "type name of " + typeName.Pkg().Path() + " " + typeName.Name()
	switch declared := typeName.Type().(type) {
	case *types.Named:
		line += renderedTypeParameters(declared.TypeParams())
	case *types.Alias:
		line += renderedTypeParameters(declared.TypeParams()) + " alias of " + writtenType(declared.Rhs())
	}
	return line + " kind " + kindOf(types.Unalias(typeName.Type()).Underlying())
}

// kindOf is the kind of an underlying type, as a use of a type's name reads it, a basic type's by which basic type it
// is: SA5008 judges a `json` tag's `string` option by the basic kind a field's pointer points to, through the pointed
// type's name alone.
func kindOf(underlying types.Type) string {
	switch underlying := underlying.(type) {
	case *types.Basic:
		return "basic " + underlying.Name()
	case *types.Pointer:
		return "pointer"
	case *types.Array:
		return "array"
	case *types.Slice:
		return "slice"
	case *types.Map:
		return "map"
	case *types.Chan:
		return "channel"
	case *types.Struct:
		return "struct"
	case *types.Signature:
		return "function"
	case *types.Interface:
		return "interface"
	default:
		return "other"
	}
}

// methodSetLines are the methods of typeName's type, and of a pointer to it unless it is an interface, that seen keeps,
// each by the set it is in, its name, its index through embedded fields, whether it is reached through a pointer, and
// its own line: what an interface's satisfaction, checked statically, reads of the type, promoted methods included.
func methodSetLines(typeName *types.TypeName, seen func(*types.Func) bool) []string {
	sets := []types.Type{typeName.Type()}
	if !types.IsInterface(typeName.Type()) {
		sets = append(sets, types.NewPointer(typeName.Type()))
	}
	var lines []string
	for _, set := range sets {
		methods := types.NewMethodSet(set)
		for index := range methods.Len() {
			selection := methods.At(index)
			method, isMethod := selection.Obj().(*types.Func)
			if !isMethod || !seen(method) {
				continue
			}
			lines = append(lines, fmt.Sprintf("method of %s %s at %v indirect %t %s", writtenType(set), method.Name(),
				selection.Index(), selection.Indirect(), ownRendering(method)))
		}
	}
	return lines
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

// closureOf is root's import closure, root and every package it reaches, walked once for the build, none of it
// indexed by the walk. The build's side of a verdict indexes only the packages a lookup reads of it
// (verdict.indexBuildCandidates): on a platform build most of the closure is restored from the host, whose type names
// stand for it, and the method sets of thousands of packages nobody reads were most of the verdict's time (#yse6t59).
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
	build.closures.Store(root, reached)
	return reached
}

// indexedClosureOf is root's import closure (closureOf), every package of it indexed by its methods' names
// (indexPackage) before it is returned, as the host's side reads it: every host candidate of a lookup's closure is
// read, so the host indexes the whole of it. A root is told indexed once, so a later verdict that asks it again
// spends one load rather than a load per package.
func (build *ReadsBuild) indexedClosureOf(root *types.Package) []*types.Package {
	reached := build.closureOf(root)
	if _, known := build.indexedRoots.Load(root); known {
		return reached
	}
	for _, each := range reached {
		build.indexPackage(each)
	}
	// Stored once every package of it is indexed, so a verdict that skips the loop finds each in the method index.
	build.indexedRoots.Store(root, true)
	return reached
}

// indexPackage adds the type names declaring declares to the method index, once for the build, as the rules that
// record a Method or Interface lookup index them (methodLookupRules): an interface by each method of its method set,
// embedded ones included, an alias's too, since initialism reads every interface by its name; and any other named
// type by each method it declares, of either receiver, never one an embedded field promotes, which no rule reads. An
// alias of a type that is not an interface is no entry: abbreviated and no-any skip every alias, and initialism reads
// interfaces alone.
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
		if types.IsInterface(typeName.Type()) {
			methods := types.NewMethodSet(typeName.Type())
			for index := range methods.Len() {
				methodName := methods.At(index).Obj().Name()
				byName[methodName] = append(byName[methodName], typeName)
			}
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if typeName.IsAlias() || !isNamed {
			continue
		}
		for method := range named.Methods() {
			byName[method.Name()] = append(byName[method.Name()], typeName)
		}
	}
	build.lock.Lock()
	defer build.lock.Unlock()
	// Another verdict may have indexed it meanwhile, and a package is indexed once. It reads as indexed only
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

// candidateKey is what one memoized candidate is asked by: a type name, for a lookup of query and name.
type candidateKey struct {
	typeName *types.TypeName
	query    string
	name     string
}

// memoizedCandidate is methodCandidateOf's answer for a candidateKey under this binary's renderer, asked with no
// answer, whether the type name is a candidate at all, and its name as a side's candidates are keyed (objectName).
type memoizedCandidate struct {
	candidate   methodCandidate
	isCandidate bool
	key         string
}

// candidate is methodCandidateOf of typeName for a lookup of query and name under this binary's renderer, rendered
// once per type name, query and name in the build, since every lookup of one name asks the same type names: on
// deeafbf4 hundreds of lookups of Clone rendered every type name with a Clone again for each (#8fbkkp4). answer, which
// only an interface's holdsAnswer reads, is read past the memo. It is the candidate, its name as a side's candidates
// are keyed, and false when typeName is no candidate.
func (build *ReadsBuild) candidate(
	typeName *types.TypeName,
	query string,
	name string,
	answer types.Object,
) (methodCandidate, string, bool) {
	key := candidateKey{typeName: typeName, query: query, name: name}
	stored, found := build.candidates.Load(key)
	if !found {
		candidate, isCandidate := methodCandidateOf(typeName, query, name, nil, ReadsRendererVersion)
		stored, _ = build.candidates.LoadOrStore(key, memoizedCandidate{candidate: candidate,
			isCandidate: isCandidate, key: objectName(typeName)})
	}
	memoized, _ := stored.(memoizedCandidate)
	if memoized.isCandidate && memoized.candidate.isInterface && answer != nil {
		if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface {
			memoized.candidate.holdsAnswer = holdsAnswer(interfaceType, answer)
		}
	}
	return memoized.candidate, memoized.key, memoized.isCandidate
}

// Restoring is one package of the host's run whose reads a verdict judges in another build.
type Restoring struct {
	// Path is the package's path, whose own objects a read never asks of the other build: its own inputs are the
	// host's there, which the portable digest compares (cache.Keyed.Portable).
	Path string

	// Imports are the paths of the packages it imports, the roots of its import closure in the other build, the same
	// as the host's while its own inputs are.
	Imports []string

	// Name is the package's name, which a PackageName lookup of an importer reads, the same in every build its own
	// inputs are the host's, and which tells the walk whether a package that changed in a build changed its closure's
	// names (untouchedOf).
	Name string

	// Reads are its reads on the host's run, for a package the host checked this run.
	Reads *build_reads.Reads

	// persisted are its reads as the entry it was restored from persisted them (persistReads), which a verdict judges
	// in place of Reads, for a package the host restored rather than checked. A package with neither is declined.
	persisted *heldReads

	// Assembly is whether it has assembly files, whose frames vet's asmdecl lays out field by field: the name, offset,
	// size and kind of every field of each argument's type, another package's unexported ones too, which no property
	// stands for. Its types' reads are judged by every type they reach rendered whole, every field's type entered.
	Assembly bool
}

// unpersistedReads is why a package with no reads, neither recorded this run nor persisted by its entry, is declined.
const unpersistedReads = "unpersisted: its reads were not persisted"

// listingsRead are the analyzers whose listings of every inherited fact are read one recorded question at a time
// (checker.Options.ListingsRead). It is no carve-out from the listing's rule but the rule's own soundness argument,
// met: the deprecated facts analyzer lists every deprecation of the import closure into its result (honnef.co/go/tools
// v0.8.1, analysis/facts/deprecated/deprecated.go:146-151), and the one rule that reads the result, deprecated_use,
// records a Deprecated lookup for every question it asks of it, found or not (its deprecationOf and
// packageDeprecationOf), so every read the listing could give is a recorded read. It cannot rot silently:
// TestOnlyTheDeprecatedFactsAreListed fails as soon as a second rule's source imports the deprecated facts, where it
// would take the result, and as soon as any other registered analyzer lists.
var listingsRead = map[*analysis.Analyzer]bool{deprecated.Analyzer: true}

// deprecatedFactType is the deprecated analyzer's fact, which its result gathers for SA1019 from every fact of the
// import closure, so a Deprecated lookup is that fact asked of the build.
var deprecatedFactType = reflect.TypeFor[*deprecated.IsDeprecated]()

// TypesHold reports whether what restoring read of other packages' types on the host's run, in host, reads the same in
// build, whose packages it reaches are closure, by path, restored those of them build restored from the host's run,
// and when it does not, the first read that differs, named by its row: T for an object its type check reached, judged
// by the kind of use it made of it, or a selection it made through another package's type, L for a rule's lookup of
// the types, and never or unknown for reads that cannot hold at all. It is the load's half of the
// verdict, decided once the package's imports are done in build and before anything of the package is read there
// (load.Restored.Holds); FactsHold is check's. A package whose own inputs are the host's and whose reads hold may take
// the host's results in build (#b2t9xmf).
//
// A package with assembly files is judged by the memos' whole twins (Restoring.Assembly), never by another package's
// unexported field rendered by its properties, and every read of it by the read object's closure, as is every read of
// a package whose reads say so (build_reads.Reads.ClosureOnly): its frames, laid out field by field, reach past what
// the type checker records.
//
// Every read fails closed: an object with no objectpath, a package the build lacks, and a lookup of a kind it does not
// know each fail it. Persisted reads are judged by the same rules (persistedTypesHold), and a package with no reads at
// all, recorded or persisted, is declined as unpersisted.
func TypesHold(
	restoring Restoring,
	closure map[string]*types.Package,
	restored map[*types.Package]bool,
	host *ReadsBuild,
	build *ReadsBuild,
) (bool, string) {
	switch {
	case restoring.persisted != nil:
		return persistedTypesHold(restoring, closure, restored, build)
	case restoring.Reads == nil:
		return false, unpersistedReads
	}
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
	closureOnly := restoring.Assembly || restoring.Reads.ClosureOnly() != ""
	for _, use := range restoring.Reads.Uses() {
		if holds, why := judged.useHolds(use, closureOnly); !holds {
			return false, "T " + why
		}
	}
	for _, selection := range restoring.Reads.Selections() {
		if holds, why := judged.selectionHolds(selection, closureOnly); !holds {
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
	switch {
	case restoring.persisted != nil:
		return persistedFactsHold(restoring, closure, build, fact, factTypes)
	case restoring.Reads == nil:
		return false, unpersistedReads
	}
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
	switch {
	case restoring.persisted != nil:
		return persistedFactTypes(restoring.persisted)
	case restoring.Reads == nil:
		return map[reflect.Type]bool{}
	}
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
