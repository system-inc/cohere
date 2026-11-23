// Package merged is the spike's importer (#yraxcp9): every package the variational check doesn't check from source
// (the standard library and third-party modules) comes through it. It reads each build's export data into that
// build's own universe, then merges the builds into one universe of the fork's objects, one object per declaration
// with its properties by build. A declared named type is one *types.Named across builds, and only its underlying type
// varies; a package-level constant, variable or function keeps one identity, and its type or value varies; an object
// declared in only some builds says which.
package merged

import (
	"fmt"
	"go/token"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"
)

// Importer merges every build's export data into one universe, a package at a time as the check imports them.
type Importer struct {
	fileSet *token.FileSet
	exports []map[string]string
	all     types.Mask
	builds  []*build // one for each build in all

	packages map[string]*types.Package // the merged universe, by path
	checked  map[string]*types.Package // packages checked from source, which stand in for the export data
	objects  map[objectKey]types.Object
	locals   map[localKey]*namedRecord
	aliases  map[*types.TypeName]*aliasRecord
	context  *types.Context // instances are shared across builds through it

	// resolving are the objects whose merge is running, so one reached again before its merge has kept it fails
	// loudly instead of waiting forever on its own lazy entry.
	resolving map[objectKey]bool
	// depth is how many resolutions are running, one inside another: only the outermost ends the merge.
	depth int

	// The work a merge leaves for its end, when every named type has its underlying type and every interface is
	// complete, since comparing types before then could read a type still being made.
	later      []func()
	interfaces []*types.Interface
	named      []*namedRecord
	variants   []*objectRecord

	shortfalls Shortfalls
	decode     time.Duration
	merge      time.Duration
}

// Shortfalls counts what the merge could not keep exactly, for the spike's report.
type Shortfalls struct {
	// KindConflicts are names declared as different kinds of object in different builds. The first build's kind is
	// kept, and the builds that declare the name as another kind read it as absent.
	KindConflicts int
	// UnboundSignatures are a generic function's or method's signatures that differ by build: a build whose
	// signature differs from the first build's reads the first's type parameters, unbound, since a type parameter can
	// be bound by one signature alone.
	UnboundSignatures int
	// StrayTypeParameters are type parameters reached outside every declaration that binds them, each made anew
	// without a binding.
	StrayTypeParameters int
	// GenericAliasVariants are generic aliases whose right-hand side differs by build, kept as the first build's.
	GenericAliasVariants int
}

// build is one build's own universe, unmerged, and how its types read in the merged universe.
type build struct {
	index      int
	mask       types.Mask
	universe   *gcimporter.Universe      // its packages, each object read the first time it is looked up
	translated map[types.Type]types.Type // memoized; a type parameter maps to the merged one its declaration binds
}

// objectKey names a package-level object across builds.
type objectKey struct {
	path string
	name string
}

// localKey names a function-local named type across builds: its package, name and where it is declared.
type localKey struct {
	path     string
	name     string
	position string
}

// namedRecord is a merged named type and what its builds said of it, compared when the merge ends.
type namedRecord struct {
	named       *types.Named
	builds      types.Mask
	underlyings []types.UnderlyingVariant
	methods     []*methodRecord
}

// methodRecord is one method of a merged named type, by name across builds.
type methodRecord struct {
	function    *types.Func
	builds      types.Mask
	declaration *declaration
}

// declaration is a declared function or method across builds: the type parameters its signature binds, made from the
// first build to reach it, that build's signature, and every later build's, read with the same type parameters.
type declaration struct {
	receiverParameters []*types.TypeParam
	typeParameters     []*types.TypeParam
	first              *types.Signature
	firstBuilds        types.Mask
	candidates         []signatureCandidate
}

// signatureCandidate is a later build's signature of a declaration, in parts, since it can bind no type parameters
// of its own until it is known to differ from the first.
type signatureCandidate struct {
	builds   types.Mask
	receiver *types.Var
	params   *types.Tuple
	results  *types.Tuple
	variadic bool
}

// aliasRecord is a merged alias: one alias type for each build that declares it, all of one type name.
type aliasRecord struct {
	byBuild map[int]*types.Alias
	folding bool // the end of the merge will give the type name its aliases by build
}

// objectRecord is a package-level object's answers by build, given to it when the merge ends.
type objectRecord struct {
	object      types.Object
	variants    []types.Variant
	declaration *declaration // a function's, whose signatures are compared as a declaration's
}

// decodeFailure carries a build's export data that couldn't be read up out of the merge, to Import's error.
type decodeFailure struct{ err error }

// New makes an importer over builds: exports[b] maps each import path build b compiles to its export data file.
func New(fileSet *token.FileSet, exports []map[string]string, all types.Mask) *Importer {
	importer := &Importer{
		fileSet:  fileSet,
		exports:  exports,
		all:      all,
		packages: map[string]*types.Package{},
		checked:  map[string]*types.Package{},
		objects:  map[objectKey]types.Object{},
		locals:   map[localKey]*namedRecord{},
		aliases:  map[*types.TypeName]*aliasRecord{},
		context:  types.NewContext(),

		resolving: map[objectKey]bool{},
	}
	for index := range exports {
		mask := types.Mask(1) << index
		if all&mask == 0 {
			continue
		}
		importer.builds = append(importer.builds, &build{
			index:      index,
			mask:       mask,
			universe:   gcimporter.NewUniverse(),
			translated: map[types.Type]types.Type{},
		})
	}
	return importer
}

// Import is path's package in the merged universe. Its scope holds a lazy entry for every name any build declares,
// and an object is merged across the builds that compile path the first time it is looked up.
func (importer *Importer) Import(path string) (pkg *types.Package, err error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if checked := importer.checked[path]; checked != nil {
		return checked, nil
	}
	if merged := importer.packages[path]; merged != nil && merged.Complete() {
		return merged, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			failure, isFailure := recovered.(decodeFailure)
			if !isFailure {
				panic(recovered)
			}
			pkg, err = nil, failure.err
		}
	}()
	importer.resolve(func() { pkg, err = importer.mergePackage(path) })
	return pkg, err
}

// resolve runs work, a merge the check asked for, outside every unit of the variational check, since a split's panic
// in the middle of a merge would leave it half done, and ends the merge once the outermost resolution is done. A
// resolution can reach others (a type's underlying type reaching another package's object), which run inside it and
// leave their comparisons to its end.
func (importer *Importer) resolve(work func()) {
	types.Outside(func() {
		if importer.depth > 0 {
			work()
			return
		}
		importer.depth++
		started := time.Now()
		decodeBefore := importer.decoded()
		defer func() {
			importer.depth--
			importer.merge += time.Since(started) - (importer.decoded() - decodeBefore)
		}()
		work()
		importer.finish()
	})
}

// Add makes checked, a package checked from source, the answer Import gives for its path.
func (importer *Importer) Add(checked *types.Package) {
	importer.checked[checked.Path()] = checked
}

// Plain is build b's own export data, unmerged, for the plain baseline. It shares build b's universe with the merge,
// so each package is decoded once whichever reads it first, and as the merge does it reads each object the first
// time the check looks it up, the time counted in Timing's decode.
func (importer *Importer) Plain(build int) types.Importer {
	for _, candidate := range importer.builds {
		if candidate.index == build {
			return plainImporter{importer: importer, build: candidate}
		}
	}
	return plainImporter{importer: importer}
}

// Timing is the time spent reading export data and the time spent merging what was read.
func (importer *Importer) Timing() (decode time.Duration, merge time.Duration) {
	return importer.decoded(), importer.merge
}

// decoded is the time spent reading export data: each package's import, and each object read on demand since, in
// every build.
func (importer *Importer) decoded() time.Duration {
	total := importer.decode
	for _, build := range importer.builds {
		total += build.universe.Elapsed
	}
	return total
}

// Shortfalls counts what the merge could not keep exactly so far.
func (importer *Importer) Shortfalls() Shortfalls { return importer.shortfalls }

// plainImporter imports one build's packages from its export data, unmerged.
type plainImporter struct {
	importer *Importer
	build    *build // nil for a build outside the importer's builds
}

func (plain plainImporter) Import(path string) (pkg *types.Package, err error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if plain.build == nil {
		return nil, fmt.Errorf("no build of this importer is asked for %q", path)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			failure, isFailure := recovered.(decodeFailure)
			if !isFailure {
				panic(recovered)
			}
			pkg, err = nil, failure.err
		}
	}()
	if !plain.importer.decodeIn(plain.build, path) {
		return nil, fmt.Errorf("build %d doesn't compile %q", plain.build.index, path)
	}
	return plain.build.universe.Packages[path], nil
}

// decodeIn reads path's export data into build's universe unless it is read already, and reports whether build
// compiles path at all.
func (importer *Importer) decodeIn(build *build, path string) bool {
	file, compiled := importer.exports[build.index][path]
	if !compiled {
		return false
	}
	if decoded := build.universe.Packages[path]; decoded != nil && decoded.Complete() {
		return true
	}
	started := time.Now()
	_, err := gcimporter.Import(importer.fileSet, build.universe, path, "", func(asked string) (io.ReadCloser, error) {
		askedFile, askedCompiled := importer.exports[build.index][asked]
		if !askedCompiled {
			return nil, fmt.Errorf("build %d has no export data for %q", build.index, asked)
		}
		return os.Open(askedFile)
	})
	importer.decode += time.Since(started)
	if err != nil {
		panic(decodeFailure{fmt.Errorf("build %d, %s: %w", build.index, file, err)})
	}
	return true
}

// declared is build's object of name at path's package level, or nil when build doesn't declare it. A package build
// has only reached through another's export data holds the objects that one refers to, so its export data is read
// only when the name isn't among them.
func (importer *Importer) declared(build *build, path string, name string) types.Object {
	if pkg := build.universe.Packages[path]; pkg != nil {
		if object := pkg.Scope().Lookup(name); object != nil || pkg.Complete() {
			return object
		}
	}
	if !importer.decodeIn(build, path) {
		return nil
	}
	return build.universe.Packages[path].Scope().Lookup(name)
}

// mergePackage gives path's merged package a lazy entry for each name the builds that compile path declare, and its
// imports, and marks it complete. No object is merged until it is looked up.
func (importer *Importer) mergePackage(path string) (*types.Package, error) {
	var names []string
	var imports []*types.Package
	var name string
	for _, build := range importer.builds {
		if !importer.decodeIn(build, path) {
			continue
		}
		decoded := build.universe.Packages[path]
		name = decoded.Name()
		names = append(names, decoded.Scope().Names()...)
		imports = append(imports, decoded.Imports()...)
	}
	if name == "" {
		return nil, fmt.Errorf("no build compiles %q", path)
	}
	merged := importer.mergedPackage(path, name)
	slices.Sort(names)
	for _, objectName := range slices.Compact(names) {
		merged.Scope().InsertLazy(objectName, importer.resolver(path, objectName))
	}

	var mergedImports []*types.Package
	for _, imported := range imports {
		mergedImports = append(mergedImports, importer.mergedPackage(imported.Path(), imported.Name()))
	}
	slices.SortFunc(mergedImports, func(left *types.Package, right *types.Package) int {
		return strings.Compare(left.Path(), right.Path())
	})
	merged.SetImports(slices.Compact(mergedImports))
	merged.MarkComplete()
	return merged, nil
}

// mergedPackage is the merged universe's package of path, made empty when it is first reached.
func (importer *Importer) mergedPackage(path string, name string) *types.Package {
	if path == "unsafe" {
		return types.Unsafe
	}
	if checked := importer.checked[path]; checked != nil {
		return checked
	}
	if merged := importer.packages[path]; merged != nil {
		return merged
	}
	merged := types.NewPackage(path, name)
	importer.packages[path] = merged
	return merged
}

// mergedPackageOf is pkg's package in the merged universe, or nil for the universe scope.
func (importer *Importer) mergedPackageOf(pkg *types.Package) *types.Package {
	if pkg == nil {
		return nil
	}
	return importer.mergedPackage(pkg.Path(), pkg.Name())
}

// object is the merged object of name at the package level of pkg (a build's package or a merged one), reached from
// a type being translated: looked up through the merged package's lazy entry, given one when the package isn't
// imported yet, or nil when no build declares it.
func (importer *Importer) object(pkg *types.Package, name string) types.Object {
	key := objectKey{path: pkg.Path(), name: name}
	if made, merged := importer.objects[key]; merged {
		return made
	}
	merged := importer.mergedPackageOf(pkg)
	if checked := importer.checked[key.path]; checked != nil {
		return checked.Scope().Lookup(name)
	}
	if importer.resolving[key] {
		// Only a type name could be reached again while it is merged, and its merge keeps it before reading anything.
		panic(fmt.Sprintf("merged: %s.%s reached again before its merge kept it", key.path, name))
	}
	declared := false
	for _, build := range importer.builds {
		if importer.declared(build, key.path, name) != nil {
			declared = true
			break
		}
	}
	if !declared {
		importer.objects[key] = nil
		return nil
	}
	merged.Scope().InsertLazy(name, importer.resolver(key.path, name))
	return merged.Scope().Lookup(name)
}

// resolver is the lazy entry of path's package-level name: the first Lookup merges the object, outside every unit.
func (importer *Importer) resolver(path string, name string) func() types.Object {
	return func() types.Object {
		var made types.Object
		importer.resolve(func() { made = importer.mergeObject(path, name) })
		if made == nil {
			panic(fmt.Sprintf("merged: no build declares %s.%s, yet it has a lazy entry", path, name))
		}
		return made
	}
}

// mergeObject merges path's package-level name across every build that declares it, keeping the merged object for
// object to find, or is nil when no build does. Its comparisons are left to the end of the resolution.
func (importer *Importer) mergeObject(path string, name string) types.Object {
	key := objectKey{path: path, name: name}
	if made, merged := importer.objects[key]; merged {
		return made
	}
	importer.resolving[key] = true
	defer delete(importer.resolving, key)

	type declared struct {
		build  *build
		object types.Object
	}
	var found []declared
	for _, build := range importer.builds {
		if object := importer.declared(build, path, name); object != nil {
			found = append(found, declared{build: build, object: object})
		}
	}
	if len(found) == 0 {
		importer.objects[key] = nil
		return nil
	}

	// A name declared as another kind of object in some builds keeps the first build's kind.
	var kept []declared
	builds := types.Mask(0)
	for _, candidate := range found {
		if objectKind(candidate.object) != objectKind(found[0].object) {
			importer.shortfalls.KindConflicts++
			continue
		}
		kept = append(kept, candidate)
		builds |= candidate.build.mask
	}
	first := kept[0].object
	pkg := importer.mergedPackageOf(first.Pkg())

	var made types.Object
	switch first := first.(type) {
	case *types.Const:
		record := &objectRecord{}
		for _, candidate := range kept {
			constant := candidate.object.(*types.Const)
			record.variants = append(record.variants, types.Variant{
				Builds: candidate.build.mask,
				Type:   importer.translate(candidate.build, constant.Type()),
				Value:  constant.Val(),
			})
		}
		made = types.NewConst(first.Pos(), pkg, name, record.variants[0].Type, record.variants[0].Value)
		record.object = made
		importer.variants = append(importer.variants, record)
	case *types.Var:
		record := &objectRecord{}
		for _, candidate := range kept {
			record.variants = append(record.variants, types.Variant{
				Builds: candidate.build.mask,
				Type:   importer.translate(candidate.build, candidate.object.Type()),
			})
		}
		made = types.NewVar(first.Pos(), pkg, name, record.variants[0].Type)
		record.object = made
		importer.variants = append(importer.variants, record)
	case *types.Func:
		record := &objectRecord{declaration: &declaration{}}
		var signature *types.Signature
		for _, candidate := range kept {
			translated := importer.declaredSignature(candidate.build, candidate.object.(*types.Func).Signature(),
				record.declaration)
			if signature == nil {
				signature = translated
			}
		}
		made = types.NewFunc(first.Pos(), pkg, name, signature)
		record.object = made
		importer.variants = append(importer.variants, record)
	case *types.TypeName:
		typeName := types.NewTypeName(first.Pos(), pkg, name, nil)
		importer.objects[key] = typeName
		if first.IsAlias() {
			importer.mergeAlias(typeName, kept[0].build, first)
			for _, candidate := range kept[1:] {
				importer.mergeAlias(typeName, candidate.build, candidate.object.(*types.TypeName))
			}
		} else {
			record := importer.startNamed(typeName, builds)
			for _, candidate := range kept {
				candidate.build.translated[candidate.object.Type()] = record.named
			}
			importer.bindNamedParameters(record, kept[0].build, first.Type().(*types.Named))
			for _, candidate := range kept[1:] {
				importer.shareParameters(candidate.build, candidate.object.Type().(*types.Named).TypeParams(),
					record.named.TypeParams())
			}
			for _, candidate := range kept {
				importer.fillNamed(record, candidate.build, candidate.object.Type().(*types.Named))
			}
		}
		if builds != importer.all {
			types.SetPresence(typeName, builds)
		}
		return typeName
	default:
		panic(fmt.Sprintf("merged: a package-level %T", first))
	}
	importer.objects[key] = made
	if builds != importer.all {
		types.SetPresence(made, builds)
	}
	return made
}

// objectKind tells package-level objects' kinds apart, an alias's type name from a defined type's.
func objectKind(object types.Object) string {
	if typeName, isTypeName := object.(*types.TypeName); isTypeName && typeName.IsAlias() {
		return "Alias"
	}
	return fmt.Sprintf("%T", object)
}

// startNamed makes typeName's one named type, before anything of it is read, since its underlying type and methods
// may refer back to it.
func (importer *Importer) startNamed(typeName *types.TypeName, builds types.Mask) *namedRecord {
	record := &namedRecord{named: types.NewNamed(typeName, nil, nil), builds: builds}
	importer.named = append(importer.named, record)
	return record
}

// bindNamedParameters gives record's named type type parameters made from build's declaration of it.
func (importer *Importer) bindNamedParameters(record *namedRecord, build *build, named *types.Named) {
	if parameters := importer.bindParameters(build, named.TypeParams()); len(parameters) > 0 {
		record.named.SetTypeParams(parameters)
	}
}

// fillNamed reads build's declaration of record's named type: its underlying type, the first build's set as the
// named type's own, and its methods, merged by name.
func (importer *Importer) fillNamed(record *namedRecord, build *build, named *types.Named) {
	underlying := importer.translate(build, named.Underlying())
	if len(record.underlyings) == 0 {
		record.named.SetUnderlying(underlying)
	}
	record.underlyings = append(record.underlyings, types.UnderlyingVariant{Builds: build.mask, Underlying: underlying})
	for index := range named.NumMethods() {
		importer.mergeMethod(record, build, named.Method(index))
	}
}

// mergeMethod adds build's method to its merged named type's method of that name, made when it is first reached.
func (importer *Importer) mergeMethod(record *namedRecord, build *build, method *types.Func) {
	for _, existing := range record.methods {
		if existing.function.Name() == method.Name() {
			existing.builds |= build.mask
			importer.declaredSignature(build, method.Signature(), existing.declaration)
			return
		}
	}
	made := &methodRecord{builds: build.mask, declaration: &declaration{}}
	signature := importer.declaredSignature(build, method.Signature(), made.declaration)
	made.function = types.NewFunc(method.Pos(), importer.mergedPackageOf(method.Pkg()), method.Name(), signature)
	record.named.AddMethod(made.function)
	record.methods = append(record.methods, made)
}

// mergeAlias reads build's declaration of the alias typeName names: an alias type of typeName for that build.
func (importer *Importer) mergeAlias(typeName *types.TypeName, build *build, declared *types.TypeName) {
	record := importer.aliases[typeName]
	if record == nil {
		record = &aliasRecord{byBuild: map[int]*types.Alias{}}
		importer.aliases[typeName] = record
	}
	alias := declared.Type().(*types.Alias)
	if len(record.byBuild) > 0 && alias.TypeParams().Len() > 0 {
		// A generic alias binds its type parameters once, so later builds read the first build's alias.
		for _, firstAlias := range record.byBuild {
			importer.shareParameters(build, alias.TypeParams(), firstAlias.TypeParams())
			record.byBuild[build.index] = firstAlias
			if !sameSpelling(importer.translate(build, alias.Rhs()), firstAlias.Rhs()) {
				importer.shortfalls.GenericAliasVariants++
			}
			return
		}
	}
	parameters := importer.bindParameters(build, alias.TypeParams())
	made := types.NewAlias(typeName, importer.translate(build, alias.Rhs()))
	if len(parameters) > 0 {
		made.SetTypeParams(parameters)
	}
	record.byBuild[build.index] = made
	if len(record.byBuild) == 1 || record.folding {
		return
	}
	record.folding = true
	importer.later = append(importer.later, func() {
		importer.foldAlias(typeName, record)
	})
}

// foldAlias gives an alias's type name its alias by build, once every build has declared it.
func (importer *Importer) foldAlias(typeName *types.TypeName, record *aliasRecord) {
	var variants []types.Variant
	for _, build := range importer.builds {
		if alias := record.byBuild[build.index]; alias != nil {
			variants = append(variants, types.Variant{Builds: build.mask, Type: alias})
		}
	}
	importer.variants = append(importer.variants, &objectRecord{object: typeName, variants: variants})
}

// sameSpelling compares two types by how they print, for a count that needs no exact answer.
func sameSpelling(left types.Type, right types.Type) bool {
	return types.TypeString(left, nil) == types.TypeString(right, nil)
}

// bindParameters makes merged type parameters for build's list, which a declaration made from build binds, and reads
// build's as them. Their constraints are read when the merge ends, since a constraint may refer to a type still being
// made.
func (importer *Importer) bindParameters(build *build, list *types.TypeParamList) []*types.TypeParam {
	if list.Len() == 0 {
		return nil
	}
	made := make([]*types.TypeParam, list.Len())
	for index := range made {
		parameter := list.At(index)
		typeName := types.NewTypeName(parameter.Obj().Pos(), importer.mergedPackageOf(parameter.Obj().Pkg()),
			parameter.Obj().Name(), nil)
		made[index] = types.NewTypeParam(typeName, nil)
		build.translated[parameter] = made[index]
	}
	importer.later = append(importer.later, func() {
		for index, parameter := range made {
			parameter.SetConstraint(importer.translate(build, list.At(index).Constraint()))
		}
	})
	return made
}

// shareParameters reads build's type parameters as shared, ones another build's declaration bound.
func (importer *Importer) shareParameters(build *build, list *types.TypeParamList, shared *types.TypeParamList) {
	for index := range min(list.Len(), shared.Len()) {
		build.translated[list.At(index)] = shared.At(index)
	}
}

// declaredSignature reads build's signature of a declared function or method. The first build to reach the
// declaration makes its signature, binding type parameters made for it; a later build reads its type parameters as
// those, and its signature is kept in parts as a candidate the end of the merge compares with the first.
func (importer *Importer) declaredSignature(build *build, signature *types.Signature,
	declared *declaration) *types.Signature {
	if declared.first == nil {
		declared.receiverParameters = importer.bindParameters(build, signature.RecvTypeParams())
		declared.typeParameters = importer.bindParameters(build, signature.TypeParams())
		declared.first = types.NewSignatureType(importer.translateVar(build, signature.Recv()),
			declared.receiverParameters, declared.typeParameters, importer.translateTuple(build, signature.Params()),
			importer.translateTuple(build, signature.Results()), signature.Variadic())
		declared.firstBuilds = build.mask
		return declared.first
	}
	importer.shareParameters(build, signature.RecvTypeParams(), declared.first.RecvTypeParams())
	importer.shareParameters(build, signature.TypeParams(), declared.first.TypeParams())
	declared.candidates = append(declared.candidates, signatureCandidate{
		builds:   build.mask,
		receiver: importer.translateVar(build, signature.Recv()),
		params:   importer.translateTuple(build, signature.Params()),
		results:  importer.translateTuple(build, signature.Results()),
		variadic: signature.Variadic(),
	})
	return declared.first
}

// signatureVariants are a declaration's signatures by build: the first build's wherever a build's reads alike.
func (importer *Importer) signatureVariants(declared *declaration) []types.Variant {
	variants := []types.Variant{{Builds: declared.firstBuilds, Type: declared.first}}
	for _, candidate := range declared.candidates {
		if candidate.variadic == declared.first.Variadic() &&
			types.Identical(candidate.params, declared.first.Params()) &&
			types.Identical(candidate.results, declared.first.Results()) {
			variants = append(variants, types.Variant{Builds: candidate.builds, Type: declared.first})
			continue
		}
		if declared.first.TypeParams().Len() > 0 || declared.first.RecvTypeParams().Len() > 0 {
			importer.shortfalls.UnboundSignatures++
		}
		variants = append(variants, types.Variant{Builds: candidate.builds, Type: types.NewSignatureType(
			candidate.receiver, nil, nil, candidate.params, candidate.results, candidate.variadic)})
	}
	return variants
}

// finish ends a merge: type parameters get their constraints and aliases their builds, interfaces are completed,
// and then, with every type whole, each named type's underlying types and each object's answers are compared by build.
//
// Each step can reach objects not merged yet, whose merges leave work of their own, so the steps run again, in order,
// until none is left.
func (importer *Importer) finish() {
	for {
		switch {
		case len(importer.later) > 0:
			later := importer.later
			importer.later = nil
			for _, run := range later {
				run()
			}
		case len(importer.interfaces) > 0:
			interfaces := importer.interfaces
			importer.interfaces = nil
			for _, completing := range interfaces {
				completing.Complete()
			}
		case len(importer.named) > 0:
			named := importer.named
			importer.named = nil
			for _, record := range named {
				importer.finishNamed(record)
			}
		case len(importer.variants) > 0:
			variants := importer.variants
			importer.variants = nil
			for _, record := range variants {
				answers := record.variants
				if record.declaration != nil {
					answers = importer.signatureVariants(record.declaration)
				}
				types.SetVariants(record.object, answers)
			}
		default:
			return
		}
	}
}

// finishNamed compares a named type's underlying types by build, and its methods' presence and signatures.
func (importer *Importer) finishNamed(record *namedRecord) {
	alike := true
	for _, variant := range record.underlyings[1:] {
		if !types.Identical(variant.Underlying, record.underlyings[0].Underlying) {
			alike = false
			break
		}
	}
	if !alike {
		types.SetUnderlyingVariants(record.named, record.underlyings)
	}
	for _, method := range record.methods {
		if method.builds != record.builds {
			types.SetPresence(method.function, method.builds)
		}
		types.SetVariants(method.function, importer.signatureVariants(method.declaration))
	}
}

// translate is build's type typed as the merged universe has it.
func (importer *Importer) translate(build *build, typed types.Type) types.Type {
	if typed == nil {
		return nil
	}
	if made, translated := build.translated[typed]; translated {
		return made
	}
	made := importer.translateUncached(build, typed)
	// An alias's answer is not kept: one reached while its own declaration is still being read stands in as its
	// target, and that must not outlast the declaration.
	if _, isAlias := typed.(*types.Alias); !isAlias {
		build.translated[typed] = made
	}
	return made
}

func (importer *Importer) translateUncached(build *build, typed types.Type) types.Type {
	switch typed := typed.(type) {
	case *types.Basic:
		return typed
	case *types.Pointer:
		return types.NewPointer(importer.translate(build, typed.Elem()))
	case *types.Slice:
		return types.NewSlice(importer.translate(build, typed.Elem()))
	case *types.Array:
		return types.NewArray(importer.translate(build, typed.Elem()), typed.Len())
	case *types.Map:
		return types.NewMap(importer.translate(build, typed.Key()), importer.translate(build, typed.Elem()))
	case *types.Chan:
		return types.NewChan(typed.Dir(), importer.translate(build, typed.Elem()))
	case *types.Struct:
		fields := make([]*types.Var, typed.NumFields())
		tags := make([]string, typed.NumFields())
		for index := range fields {
			field := typed.Field(index)
			fields[index] = types.NewField(field.Pos(), importer.mergedPackageOf(field.Pkg()), field.Name(),
				importer.translate(build, field.Type()), field.Embedded())
			tags[index] = typed.Tag(index)
		}
		return types.NewStruct(fields, tags)
	case *types.Tuple:
		return importer.translateTuple(build, typed)
	case *types.Signature:
		receiverParameters := importer.bindParameters(build, typed.RecvTypeParams())
		typeParameters := importer.bindParameters(build, typed.TypeParams())
		return types.NewSignatureType(importer.translateVar(build, typed.Recv()), receiverParameters, typeParameters,
			importer.translateTuple(build, typed.Params()), importer.translateTuple(build, typed.Results()),
			typed.Variadic())
	case *types.Interface:
		return importer.translateInterface(build, typed)
	case *types.Union:
		terms := make([]*types.Term, typed.Len())
		for index := range terms {
			term := typed.Term(index)
			terms[index] = types.NewTerm(term.Tilde(), importer.translate(build, term.Type()))
		}
		return types.NewUnion(terms)
	case *types.TypeParam:
		// Every type parameter is read as the one its declaration bound, so one reached here was bound by nothing
		// the merge read.
		importer.shortfalls.StrayTypeParameters++
		made := types.NewTypeParam(types.NewTypeName(typed.Obj().Pos(), importer.mergedPackageOf(typed.Obj().Pkg()),
			typed.Obj().Name(), nil), nil)
		build.translated[typed] = made
		importer.later = append(importer.later, func() {
			made.SetConstraint(importer.translate(build, typed.Constraint()))
		})
		return made
	case *types.Named:
		return importer.translateNamed(build, typed)
	case *types.Alias:
		return importer.translateAlias(build, typed)
	}
	panic(fmt.Sprintf("merged: a %T", typed))
}

// translateTuple is build's tuple as the merged universe has it.
func (importer *Importer) translateTuple(build *build, tuple *types.Tuple) *types.Tuple {
	if tuple.Len() == 0 {
		return nil
	}
	variables := make([]*types.Var, tuple.Len())
	for index := range variables {
		variables[index] = importer.translateVar(build, tuple.At(index))
	}
	return types.NewTuple(variables...)
}

// translateVar is build's parameter, result or receiver as the merged universe has it.
func (importer *Importer) translateVar(build *build, variable *types.Var) *types.Var {
	if variable == nil {
		return nil
	}
	made := types.NewParam(variable.Pos(), importer.mergedPackageOf(variable.Pkg()), variable.Name(),
		importer.translate(build, variable.Type()))
	made.SetKind(variable.Kind())
	return made
}

// translateInterface is build's interface as the merged universe has it. A method's receiver is kept when it is a
// named type's, as the reader gives a named interface's methods, and otherwise left for NewInterfaceType to set, since
// reading the interface itself as a receiver would never end.
func (importer *Importer) translateInterface(build *build, iface *types.Interface) *types.Interface {
	methods := make([]*types.Func, iface.NumExplicitMethods())
	for index := range methods {
		method := iface.ExplicitMethod(index)
		signature := method.Signature()
		var receiver *types.Var
		if signature.Recv() != nil {
			if _, isNamed := types.Unalias(signature.Recv().Type()).(*types.Named); isNamed {
				receiver = importer.translateVar(build, signature.Recv())
			}
		}
		methods[index] = types.NewFunc(method.Pos(), importer.mergedPackageOf(method.Pkg()), method.Name(),
			types.NewSignatureType(receiver, nil, nil, importer.translateTuple(build, signature.Params()),
				importer.translateTuple(build, signature.Results()), signature.Variadic()))
	}
	embeddeds := make([]types.Type, iface.NumEmbeddeds())
	for index := range embeddeds {
		embeddeds[index] = importer.translate(build, iface.EmbeddedType(index))
	}
	made := types.NewInterfaceType(methods, embeddeds)
	if iface.IsImplicit() {
		made.MarkImplicit()
	}
	importer.interfaces = append(importer.interfaces, made)
	return made
}

// translateNamed is build's named type as the merged universe has it: an instance by its origin and type arguments, a
// package-level type by its package and name, a function-local one by where it is declared.
func (importer *Importer) translateNamed(build *build, named *types.Named) types.Type {
	if named.TypeArgs().Len() > 0 {
		return importer.instantiate(build, importer.translate(build, named.Origin()), named.TypeArgs())
	}
	typeName := named.Obj()
	if typeName.Pkg() == nil {
		return named // error and comparable, the universe's own
	}
	if typeName.Parent() == typeName.Pkg().Scope() {
		object := importer.object(typeName.Pkg(), typeName.Name())
		if object == nil {
			panic(fmt.Sprintf("merged: %s.%s is in no build's scope", typeName.Pkg().Path(), typeName.Name()))
		}
		return object.Type()
	}
	key := localKey{path: typeName.Pkg().Path(), name: typeName.Name(),
		position: importer.fileSet.Position(typeName.Pos()).String()}
	record := importer.locals[key]
	if record == nil {
		record = importer.startNamed(types.NewTypeName(typeName.Pos(), importer.mergedPackageOf(typeName.Pkg()),
			typeName.Name(), nil), 0)
		importer.locals[key] = record
		build.translated[named] = record.named
		importer.bindNamedParameters(record, build, named)
	} else {
		build.translated[named] = record.named
		importer.shareParameters(build, named.TypeParams(), record.named.TypeParams())
	}
	record.builds |= build.mask
	importer.fillNamed(record, build, named)
	return record.named
}

// translateAlias is build's alias as the merged universe has it: an instance by its origin and type arguments, a
// package-level alias by its package and name, and a function-local one as its target.
func (importer *Importer) translateAlias(build *build, alias *types.Alias) types.Type {
	if alias.TypeArgs().Len() > 0 {
		return importer.instantiate(build, importer.translate(build, alias.Origin()), alias.TypeArgs())
	}
	typeName := alias.Obj()
	if typeName.Pkg() == nil {
		return alias // any, the universe's own
	}
	if typeName.Parent() != typeName.Pkg().Scope() {
		return importer.translate(build, alias.Rhs())
	}
	merged, isTypeName := importer.object(typeName.Pkg(), typeName.Name()).(*types.TypeName)
	if !isTypeName {
		panic(fmt.Sprintf("merged: %s.%s is in no build's scope", typeName.Pkg().Path(), typeName.Name()))
	}
	record := importer.aliases[merged]
	if record == nil {
		// The name is a defined type in the first build that declares it, a kind conflict counted there.
		return merged.Type()
	}
	if made := record.byBuild[build.index]; made != nil {
		return made
	}
	// The alias's own declaration is still being read: its target stands in.
	return importer.translate(build, alias.Rhs())
}

// instantiate is origin, a merged generic type, instantiated with build's type arguments, shared across builds through
// the importer's context.
func (importer *Importer) instantiate(build *build, origin types.Type, arguments *types.TypeList) types.Type {
	translated := make([]types.Type, arguments.Len())
	for index := range translated {
		translated[index] = importer.translate(build, arguments.At(index))
	}
	// Without validation Instantiate reports no error: only a count of arguments that differs from the
	// parameters' panics, and the merged origin has the declaration's own.
	instance, err := types.Instantiate(importer.context, origin, translated, false)
	if err != nil {
		panic(fmt.Sprintf("merged: instantiating %s: %v", origin, err))
	}
	return instance
}
