package merged

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/go-types/types"
)

// interfaceRecord is a translated interface, the build it was read from and the build's own interface, completed when
// the merge ends.
type interfaceRecord struct {
	made   *types.Interface
	build  *build
	source *types.Interface
}

// resolution is one merge the check asked for, and the work it leaves for its end, when every named type has its
// underlying type and every interface is complete, since comparing types before then could read a type still being
// made. A resolution runs whole under the importer's lock.
type resolution struct {
	importer   *Importer
	later      []func()
	interfaces []interfaceRecord
	named      []*namedRecord
	variants   []*objectRecord
	// placed are the objects merged for lazy entries this resolution didn't resolve, given to their entries once it
	// ends, so no lookup without the lock reads an object still being made.
	placed []placedObject
}

// placedObject is a merged object for its lazy entry.
type placedObject struct {
	entry *lazyEntry
	made  types.Object
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
	builds     types.Mask
	receiver   *types.Var
	parameters *types.Tuple
	results    *types.Tuple
	variadic   bool
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

// readEach runs work for every build at once, each behind its build's lock, and notes each build's failure as one
// met merging key. It is whether each build's work succeeded.
//
// Requires: the importer's lock is held.
func (resolution *resolution) readEach(key objectKey, work func(index int, build *build) error) []bool {
	importer := resolution.importer
	started := time.Now()
	failures := make([]error, len(importer.builds))
	var group sync.WaitGroup
	for index, build := range importer.builds {
		group.Go(func() {
			failures[index] = build.read(func() error { return work(index, build) })
		})
	}
	group.Wait()
	importer.decodeWait += time.Since(started)
	succeeded := make([]bool, len(importer.builds))
	for index, failure := range failures {
		succeeded[index] = failure == nil
		if failure != nil {
			importer.noteFailure(importer.builds[index], key, failure)
		}
	}
	return succeeded
}

// readBuild runs work, a read of build's types, behind build's lock, and is whether it succeeded, noting a failure as
// one met merging key.
//
// Requires: the importer's lock is held.
func (resolution *resolution) readBuild(build *build, key objectKey, work func()) bool {
	importer := resolution.importer
	started := time.Now()
	failure := build.read(func() error {
		work()
		return nil
	})
	importer.decodeWait += time.Since(started)
	if failure != nil {
		importer.noteFailure(build, key, failure)
		return false
	}
	return true
}

// mergePackage gives path's merged package a lazy entry for each name the builds that compile path declare, and its
// imports, and marks it complete. Every build's export data for path is read at once. No object is merged until it
// is looked up.
func (resolution *resolution) mergePackage(path string) (*types.Package, error) {
	importer := resolution.importer
	type decodedPackage struct {
		name    string
		names   []string
		imports []*types.Package
	}
	decoded := make([]decodedPackage, len(importer.builds))
	succeeded := resolution.readEach(objectKey{path: path}, func(index int, build *build) error {
		importedPackage, importError := build.source.Import(path)
		if importError != nil || importedPackage == nil {
			return importError
		}
		decoded[index] = decodedPackage{name: importedPackage.Name(), names: importedPackage.Scope().Names(),
			imports: importedPackage.Imports()}
		return nil
	})
	var names []string
	var imports []*types.Package
	var name string
	var failures []error
	for index, each := range decoded {
		if !succeeded[index] {
			failures = append(failures, fmt.Errorf("build %d couldn't read %q", importer.builds[index].index, path))
			continue
		}
		if each.name == "" {
			continue
		}
		name = each.name
		names = append(names, each.names...)
		imports = append(imports, each.imports...)
	}
	if name == "" {
		if len(failures) > 0 {
			return nil, errors.Join(failures...)
		}
		return nil, fmt.Errorf("no build compiles %q", path)
	}
	merged := importer.mergedPackage(path, name)
	slices.Sort(names)
	for _, objectName := range slices.Compact(names) {
		key := objectKey{path: path, name: objectName}
		entry := &lazyEntry{}
		if merged.Scope().InsertLazy(objectName, importer.resolver(entry, key)) {
			importer.entries[key] = entry
		}
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

// object is the merged object of name at the package level of objectPackage (a build's package or a merged one),
// reached from
// a type being translated, merged now when it isn't yet, or nil when no build declares it.
func (resolution *resolution) object(objectPackage *types.Package, name string) types.Object {
	resolution.importer.mergedPackageOf(objectPackage)
	return resolution.objectAt(objectKey{path: objectPackage.Path(), name: name})
}

// objectAt is key's merged object, merged now when it isn't yet, or nil when no build declares it. The object is put
// in its merged package's scope: given to its lazy entry once the resolution has ended, and the entry gives it without
// the importer's lock when it is looked up, or inserted when the package isn't imported yet and so has no entries.
// The entry is never resolved here: another goroutine's lookup may be resolving it, waiting on the lock this merge
// holds.
func (resolution *resolution) objectAt(key objectKey) types.Object {
	importer := resolution.importer
	if made, merged := importer.objects[key]; merged {
		return made
	}
	if checked := importer.checked[key.path]; checked != nil {
		return checked.Scope().Lookup(key.name)
	}
	if importer.resolving[key] {
		// Invariant: only a type name is reached again while it is merged, and its merge keeps it in objects before
		// reading anything of it.
		panic(fmt.Sprintf("merged: %s.%s reached again before its merge kept it", key.path, key.name))
	}
	made := resolution.mergeObject(key.path, key.name)
	if made == nil {
		return nil
	}
	if entry := importer.entries[key]; entry != nil {
		resolution.placed = append(resolution.placed, placedObject{entry: entry, made: made})
	} else if merged := importer.packages[key.path]; !merged.Complete() {
		merged.Scope().Insert(made)
	}
	return made
}

// mergeObject merges path's package-level name across every build that declares it, keeping the merged object for
// object to find, or is nil when no build does. Every build's object is read at once, a defined type's declaration
// with it, since the merge reads that next. A build whose export data couldn't be read reads the object as absent,
// and when no build could, the object is an invalid one. Its comparisons are left to the end of the resolution.
func (resolution *resolution) mergeObject(path string, name string) types.Object {
	key := objectKey{path: path, name: name}
	if made, merged := resolution.importer.objects[key]; merged {
		return made
	}
	resolution.importer.resolving[key] = true
	defer delete(resolution.importer.resolving, key)

	objects := make([]types.Object, len(resolution.importer.builds))
	succeeded := resolution.readEach(key, func(index int, build *build) error {
		object, readError := build.declared(path, name)
		if readError != nil {
			return readError
		}
		if typeName, isTypeName := object.(*types.TypeName); isTypeName && !typeName.IsAlias() {
			if named, isNamed := typeName.Type().(*types.Named); isNamed {
				named.NumMethods()
			}
		}
		objects[index] = object
		return nil
	})
	var found []declared
	failed := false
	for index, object := range objects {
		failed = failed || !succeeded[index]
		if object != nil {
			found = append(found, declared{build: resolution.importer.builds[index], object: object})
		}
	}
	if len(found) == 0 {
		var made types.Object
		if failed {
			made = invalidObject(resolution.importer.mergedPackage(path, ""), name)
		}
		resolution.importer.objects[key] = made
		return made
	}

	// A name declared as another kind of object in some builds keeps the first build's kind.
	var kept []declared
	builds := types.Mask(0)
	for _, candidate := range found {
		if objectKind(candidate.object) != objectKind(found[0].object) {
			resolution.importer.shortfalls.KindConflicts++
			continue
		}
		kept = append(kept, candidate)
		builds |= candidate.build.mask
	}
	first := kept[0].object
	mergedObjectPackage := resolution.importer.mergedPackageOf(first.Pkg())

	var made types.Object
	switch first := first.(type) {
	case *types.Const:
		record := &objectRecord{}
		for _, candidate := range kept {
			constant, isConstant := candidate.object.(*types.Const)
			if !isConstant {
				continue // never: every kept object is of the first's kind
			}
			record.variants = append(record.variants, types.Variant{
				Builds: candidate.build.mask,
				Type:   resolution.translate(candidate.build, constant.Type()),
				Value:  constant.Val(),
			})
		}
		made = types.NewConst(first.Pos(), mergedObjectPackage, name, record.variants[0].Type, record.variants[0].Value)
		record.object = made
		resolution.variants = append(resolution.variants, record)
	case *types.Var:
		record := &objectRecord{}
		for _, candidate := range kept {
			record.variants = append(record.variants, types.Variant{
				Builds: candidate.build.mask,
				Type:   resolution.translate(candidate.build, candidate.object.Type()),
			})
		}
		made = types.NewVar(first.Pos(), mergedObjectPackage, name, record.variants[0].Type)
		record.object = made
		resolution.variants = append(resolution.variants, record)
	case *types.Func:
		record := &objectRecord{declaration: &declaration{}}
		var signature *types.Signature
		for _, candidate := range kept {
			function, isFunction := candidate.object.(*types.Func)
			if !isFunction {
				continue // never: every kept object is of the first's kind
			}
			translated := resolution.declaredSignature(candidate.build, function.Signature(), record.declaration)
			if signature == nil {
				signature = translated
			}
		}
		made = types.NewFunc(first.Pos(), mergedObjectPackage, name, signature)
		record.object = made
		resolution.variants = append(resolution.variants, record)
	case *types.TypeName:
		typeName := types.NewTypeName(first.Pos(), mergedObjectPackage, name, nil)
		resolution.importer.objects[key] = typeName
		if first.IsAlias() {
			for _, candidate := range kept {
				if declared, isTypeName := candidate.object.(*types.TypeName); isTypeName {
					resolution.mergeAlias(typeName, candidate.build, declared)
				}
			}
		} else {
			resolution.mergeNamed(typeName, builds, kept)
		}
		if builds != resolution.importer.all {
			types.SetPresence(typeName, builds)
		}
		return typeName
	default:
		// Invariant: a package's scope holds constants, variables, type names and functions, and nothing else.
		panic(fmt.Sprintf("merged: a package-level %T", first))
	}
	resolution.importer.objects[key] = made
	if builds != resolution.importer.all {
		types.SetPresence(made, builds)
	}
	return made
}

// mergeNamed merges the defined type typeName names from each build's declaration of it in kept, which declare it in
// builds: one named type, its type parameters made from the first build's declaration and read by every later
// build's, and each build's underlying type and methods.
func (resolution *resolution) mergeNamed(typeName *types.TypeName, builds types.Mask, kept []declared) {
	type declaredNamed struct {
		build *build
		named *types.Named
	}
	var declarations []declaredNamed
	for _, candidate := range kept {
		if named, isNamed := candidate.object.Type().(*types.Named); isNamed {
			declarations = append(declarations, declaredNamed{build: candidate.build, named: named})
		}
	}
	if len(declarations) == 0 {
		return // never: a defined type's type is a named type
	}
	record := resolution.startNamed(typeName, builds)
	for _, declaration := range declarations {
		declaration.build.translated[declaration.named] = record.named
	}
	resolution.bindNamedParameters(record, declarations[0].build, declarations[0].named)
	for _, declaration := range declarations[1:] {
		resolution.shareParameters(declaration.build, resolution.typeParameters(declaration.build, declaration.named),
			record.named.TypeParams())
	}
	for _, declaration := range declarations {
		resolution.fillNamed(record, declaration.build, declaration.named)
	}
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
func (resolution *resolution) startNamed(typeName *types.TypeName, builds types.Mask) *namedRecord {
	record := &namedRecord{named: types.NewNamed(typeName, nil, nil), builds: builds}
	resolution.named = append(resolution.named, record)
	return record
}

// bindNamedParameters gives record's named type type parameters made from build's declaration of it.
func (resolution *resolution) bindNamedParameters(record *namedRecord, build *build, named *types.Named) {
	if parameters := resolution.bindParameters(build, resolution.typeParameters(build, named)); len(parameters) > 0 {
		record.named.SetTypeParams(parameters)
	}
}

// typeParameters is build's named type's type parameters, read behind build's lock, since reading them can read the
// named type's declaration; none when the build couldn't read it.
func (resolution *resolution) typeParameters(build *build, named *types.Named) *types.TypeParamList {
	var list *types.TypeParamList
	typeName := named.Obj()
	resolution.readBuild(build, objectKey{path: typeName.Pkg().Path(), name: typeName.Name()}, func() {
		list = named.TypeParams()
	})
	return list
}

// fillNamed reads build's declaration of record's named type: its underlying type, the first build's set as the
// named type's own, and its methods, merged by name. A declaration the build couldn't read is the invalid type, with
// no methods.
//
// Mutates: record gets build's underlying type and methods.
func (resolution *resolution) fillNamed(record *namedRecord, build *build, named *types.Named) {
	var declaredUnderlying types.Type = types.Typ[types.Invalid]
	var methods []*types.Func
	typeName := record.named.Obj()
	resolution.readBuild(build, objectKey{path: typeName.Pkg().Path(), name: typeName.Name()}, func() {
		declaredUnderlying = named.Underlying()
		for index := range named.NumMethods() {
			methods = append(methods, named.Method(index))
		}
	})
	underlying := resolution.translate(build, declaredUnderlying)
	if len(record.underlyings) == 0 {
		record.named.SetUnderlying(underlying)
	}
	record.underlyings = append(record.underlyings, types.UnderlyingVariant{Builds: build.mask, Underlying: underlying})
	for _, method := range methods {
		resolution.mergeMethod(record, build, method)
	}
}

// mergeMethod adds build's method to its merged named type's method of that name, made when it is first reached.
//
// Mutates: record gets the method, or the method of its name gets build.
func (resolution *resolution) mergeMethod(record *namedRecord, build *build, method *types.Func) {
	for _, existing := range record.methods {
		if existing.function.Name() == method.Name() {
			existing.builds |= build.mask
			resolution.declaredSignature(build, method.Signature(), existing.declaration)
			return
		}
	}
	made := &methodRecord{builds: build.mask, declaration: &declaration{}}
	signature := resolution.declaredSignature(build, method.Signature(), made.declaration)
	made.function = types.NewFunc(
		method.Pos(),
		resolution.importer.mergedPackageOf(method.Pkg()),
		method.Name(),
		signature,
	)
	record.named.AddMethod(made.function)
	record.methods = append(record.methods, made)
}

// mergeAlias reads build's declaration of the alias typeName names: an alias type of typeName for that build.
func (resolution *resolution) mergeAlias(typeName *types.TypeName, build *build, declared *types.TypeName) {
	record := resolution.importer.aliases[typeName]
	if record == nil {
		record = &aliasRecord{byBuild: map[int]*types.Alias{}}
		resolution.importer.aliases[typeName] = record
	}
	alias, isAlias := declared.Type().(*types.Alias)
	if !isAlias {
		return // never: the reader makes every alias an alias type
	}
	if len(record.byBuild) > 0 && alias.TypeParams().Len() > 0 {
		// A generic alias binds its type parameters once, so later builds read the first build's alias.
		for _, firstAlias := range record.byBuild {
			resolution.shareParameters(build, alias.TypeParams(), firstAlias.TypeParams())
			record.byBuild[build.index] = firstAlias
			if !sameSpelling(resolution.translate(build, alias.Rhs()), firstAlias.Rhs()) {
				resolution.importer.shortfalls.GenericAliasVariants++
			}
			return
		}
	}
	parameters := resolution.bindParameters(build, alias.TypeParams())
	made := types.NewAlias(typeName, resolution.translate(build, alias.Rhs()))
	if len(parameters) > 0 {
		made.SetTypeParams(parameters)
	}
	record.byBuild[build.index] = made
	if len(record.byBuild) == 1 || record.folding {
		return
	}
	record.folding = true
	resolution.later = append(resolution.later, func() {
		resolution.foldAlias(typeName, record)
	})
}

// foldAlias gives an alias's type name its alias by build, once every build has declared it.
func (resolution *resolution) foldAlias(typeName *types.TypeName, record *aliasRecord) {
	var variants []types.Variant
	for _, build := range resolution.importer.builds {
		if alias := record.byBuild[build.index]; alias != nil {
			variants = append(variants, types.Variant{Builds: build.mask, Type: alias})
		}
	}
	resolution.variants = append(resolution.variants, &objectRecord{object: typeName, variants: variants})
}

// sameSpelling compares two types by how they print, for a count that needs no exact answer.
func sameSpelling(left types.Type, right types.Type) bool {
	return types.TypeString(left, nil) == types.TypeString(right, nil)
}

// bindParameters makes merged type parameters for build's list, which a declaration made from build binds, and reads
// build's as them. Their constraints are read when the merge ends, since a constraint may refer to a type still being
// made.
func (resolution *resolution) bindParameters(build *build, list *types.TypeParamList) []*types.TypeParam {
	if list.Len() == 0 {
		return nil
	}
	made := make([]*types.TypeParam, list.Len())
	for index := range made {
		parameter := list.At(index)
		typeName := types.NewTypeName(parameter.Obj().Pos(), resolution.importer.mergedPackageOf(parameter.Obj().Pkg()),
			parameter.Obj().Name(), nil)
		made[index] = types.NewTypeParam(typeName, nil)
		build.translated[parameter] = made[index]
	}
	resolution.later = append(resolution.later, func() {
		for index, parameter := range made {
			parameter.SetConstraint(resolution.translate(build, list.At(index).Constraint()))
		}
	})
	return made
}

// shareParameters reads build's type parameters as shared, ones another build's declaration bound.
func (resolution *resolution) shareParameters(build *build, list *types.TypeParamList, shared *types.TypeParamList) {
	for index := range min(list.Len(), shared.Len()) {
		build.translated[list.At(index)] = shared.At(index)
	}
}

// declaredSignature reads build's signature of a declared function or method. The first build to reach the
// declaration makes its signature, binding type parameters made for it; a later build reads its type parameters as
// those, and its signature is kept in parts as a candidate the end of the merge compares with the first.
//
// Mutates: declared gets build's signature, as its first or a candidate.
func (resolution *resolution) declaredSignature(build *build, signature *types.Signature,
	declared *declaration) *types.Signature {
	if declared.first == nil {
		declared.receiverParameters = resolution.bindParameters(build, signature.RecvTypeParams())
		declared.typeParameters = resolution.bindParameters(build, signature.TypeParams())
		declared.first = types.NewSignatureType(resolution.translateVariable(build, signature.Recv()),
			declared.receiverParameters, declared.typeParameters, resolution.translateTuple(build, signature.Params()),
			resolution.translateTuple(build, signature.Results()), signature.Variadic())
		declared.firstBuilds = build.mask
		return declared.first
	}
	resolution.shareParameters(build, signature.RecvTypeParams(), declared.first.RecvTypeParams())
	resolution.shareParameters(build, signature.TypeParams(), declared.first.TypeParams())
	declared.candidates = append(declared.candidates, signatureCandidate{
		builds:     build.mask,
		receiver:   resolution.translateVariable(build, signature.Recv()),
		parameters: resolution.translateTuple(build, signature.Params()),
		results:    resolution.translateTuple(build, signature.Results()),
		variadic:   signature.Variadic(),
	})
	return declared.first
}

// signatureVariants are a declaration's signatures by build: the first build's wherever a build's reads alike.
func (resolution *resolution) signatureVariants(declared *declaration) []types.Variant {
	variants := []types.Variant{{Builds: declared.firstBuilds, Type: declared.first}}
	for _, candidate := range declared.candidates {
		if candidate.variadic == declared.first.Variadic() &&
			types.Identical(candidate.parameters, declared.first.Params()) &&
			types.Identical(candidate.results, declared.first.Results()) {
			variants = append(variants, types.Variant{Builds: candidate.builds, Type: declared.first})
			continue
		}
		if declared.first.TypeParams().Len() > 0 || declared.first.RecvTypeParams().Len() > 0 {
			resolution.importer.shortfalls.UnboundSignatures++
		}
		variants = append(variants, types.Variant{Builds: candidate.builds, Type: types.NewSignatureType(
			candidate.receiver, nil, nil, candidate.parameters, candidate.results, candidate.variadic)})
	}
	return variants
}

// finish ends a merge: type parameters get their constraints and aliases their builds, interfaces are completed,
// and then, with every type whole, each named type's underlying types and each object's answers are compared by build.
//
// Each step can reach objects not merged yet, whose merges leave work of their own, so the steps run again, in order,
// until none is left.
func (resolution *resolution) finish() {
	for {
		switch {
		case len(resolution.later) > 0:
			later := resolution.later
			resolution.later = nil
			for _, run := range later {
				run()
			}
		case len(resolution.interfaces) > 0:
			interfaces := resolution.interfaces
			resolution.interfaces = nil
			for _, completing := range interfaces {
				resolution.complete(completing)
			}
		case len(resolution.named) > 0:
			named := resolution.named
			resolution.named = nil
			for _, record := range named {
				resolution.finishNamed(record)
			}
		case len(resolution.variants) > 0:
			variants := resolution.variants
			resolution.variants = nil
			for _, record := range variants {
				answers := record.variants
				if record.declaration != nil {
					answers = resolution.signatureVariants(record.declaration)
				}
				types.SetVariants(record.object, answers)
			}
		default:
			return
		}
	}
}

// finishNamed compares a named type's underlying types by build, and its methods' presence and signatures.
func (resolution *resolution) finishNamed(record *namedRecord) {
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
		types.SetVariants(method.function, resolution.signatureVariants(method.declaration))
	}
}

// translate is build's type typed as the merged universe has it.
func (resolution *resolution) translate(build *build, typed types.Type) types.Type {
	if typed == nil {
		return nil
	}
	if made, translated := build.translated[typed]; translated {
		return made
	}
	made := resolution.translateUncached(build, typed)
	// An alias's answer is not kept: one reached while its own declaration is still being read stands in as its
	// target, and that must not outlast the declaration.
	if _, isAlias := typed.(*types.Alias); !isAlias {
		build.translated[typed] = made
	}
	return made
}

func (resolution *resolution) translateUncached(build *build, typed types.Type) types.Type {
	switch typed := typed.(type) {
	case *types.Basic:
		return typed
	case *types.Pointer:
		return types.NewPointer(resolution.translate(build, typed.Elem()))
	case *types.Slice:
		return types.NewSlice(resolution.translate(build, typed.Elem()))
	case *types.Array:
		return types.NewArray(resolution.translate(build, typed.Elem()), typed.Len())
	case *types.Map:
		return types.NewMap(resolution.translate(build, typed.Key()), resolution.translate(build, typed.Elem()))
	case *types.Chan:
		return types.NewChan(typed.Dir(), resolution.translate(build, typed.Elem()))
	case *types.Struct:
		fields := make([]*types.Var, typed.NumFields())
		tags := make([]string, typed.NumFields())
		for index := range fields {
			field := typed.Field(index)
			fields[index] = types.NewField(field.Pos(), resolution.importer.mergedPackageOf(field.Pkg()), field.Name(),
				resolution.translate(build, field.Type()), field.Embedded())
			tags[index] = typed.Tag(index)
		}
		return types.NewStruct(fields, tags)
	case *types.Tuple:
		return resolution.translateTuple(build, typed)
	case *types.Signature:
		receiverParameters := resolution.bindParameters(build, typed.RecvTypeParams())
		typeParameters := resolution.bindParameters(build, typed.TypeParams())
		return types.NewSignatureType(resolution.translateVariable(build, typed.Recv()), receiverParameters,
			typeParameters, resolution.translateTuple(build, typed.Params()),
			resolution.translateTuple(build, typed.Results()), typed.Variadic())
	case *types.Interface:
		return resolution.translateInterface(build, typed)
	case *types.Union:
		terms := make([]*types.Term, typed.Len())
		for index := range terms {
			term := typed.Term(index)
			terms[index] = types.NewTerm(term.Tilde(), resolution.translate(build, term.Type()))
		}
		return types.NewUnion(terms)
	case *types.TypeParam:
		// Every type parameter is read as the one its declaration bound, so one reached here was bound by nothing
		// the merge read.
		resolution.importer.shortfalls.StrayTypeParameters++
		made := types.NewTypeParam(
			types.NewTypeName(typed.Obj().Pos(), resolution.importer.mergedPackageOf(typed.Obj().Pkg()),
				typed.Obj().Name(), nil),
			nil,
		)
		build.translated[typed] = made
		resolution.later = append(resolution.later, func() {
			made.SetConstraint(resolution.translate(build, typed.Constraint()))
		})
		return made
	case *types.Named:
		return resolution.translateNamed(build, typed)
	case *types.Alias:
		return resolution.translateAlias(build, typed)
	}
	// Invariant: the cases above are every kind of type go/types makes.
	panic(fmt.Sprintf("merged: a %T", typed))
}

// translateTuple is build's tuple as the merged universe has it.
func (resolution *resolution) translateTuple(build *build, tuple *types.Tuple) *types.Tuple {
	if tuple.Len() == 0 {
		return nil
	}
	variables := make([]*types.Var, tuple.Len())
	for index := range variables {
		variables[index] = resolution.translateVariable(build, tuple.At(index))
	}
	return types.NewTuple(variables...)
}

// translateVariable is build's parameter, result or receiver as the merged universe has it.
func (resolution *resolution) translateVariable(build *build, variable *types.Var) *types.Var {
	if variable == nil {
		return nil
	}
	made := types.NewParam(variable.Pos(), resolution.importer.mergedPackageOf(variable.Pkg()), variable.Name(),
		resolution.translate(build, variable.Type()))
	made.SetKind(variable.Kind())
	return made
}

// translateInterface is build's interface as the merged universe has it. A method's receiver is kept when it is a
// named type's, as the reader gives a named interface's methods, and otherwise left for NewInterfaceType to set, since
// reading the interface itself as a receiver would never end.
func (resolution *resolution) translateInterface(build *build, iface *types.Interface) *types.Interface {
	methods := make([]*types.Func, iface.NumExplicitMethods())
	for index := range methods {
		methods[index] = resolution.translateInterfaceMethod(build, iface.ExplicitMethod(index))
	}
	embeddeds := make([]types.Type, iface.NumEmbeddeds())
	for index := range embeddeds {
		embeddeds[index] = resolution.translate(build, iface.EmbeddedType(index))
	}
	made := types.NewInterfaceType(methods, embeddeds)
	if iface.IsImplicit() {
		made.MarkImplicit()
	}
	resolution.interfaces = append(resolution.interfaces, interfaceRecord{made: made, build: build, source: iface})
	return made
}

// translateInterfaceMethod is build's method of an interface as the merged universe has it, its receiver kept when it
// is a named type's.
func (resolution *resolution) translateInterfaceMethod(build *build, method *types.Func) *types.Func {
	signature := method.Signature()
	var receiver *types.Var
	if signature.Recv() != nil {
		if _, isNamed := types.Unalias(signature.Recv().Type()).(*types.Named); isNamed {
			receiver = resolution.translateVariable(build, signature.Recv())
		}
	}
	return types.NewFunc(method.Pos(), resolution.importer.mergedPackageOf(method.Pkg()), method.Name(),
		types.NewSignatureType(receiver, nil, nil, resolution.translateTuple(build, signature.Params()),
			resolution.translateTuple(build, signature.Results()), signature.Variadic()))
}

// complete completes a translated interface as the build it was read from has it. One that embeds others is given
// its build's own method set, read from the build's interface, since the merged interfaces it embeds may differ by
// build and, read now outside every unit, would answer for another build. A constraint's type terms can't be given
// so, and one that embeds others is completed from the merged view, a shortfall.
func (resolution *resolution) complete(record interfaceRecord) {
	if record.made.NumEmbeddeds() == 0 {
		record.made.Complete()
		return
	}
	isMethodSet := false
	var methods []*types.Func
	typeSetRead := resolution.readBuild(record.build, objectKey{}, func() {
		isMethodSet = record.source.IsMethodSet()
		for index := range record.source.NumMethods() {
			methods = append(methods, record.source.Method(index))
		}
	})
	if !typeSetRead || !isMethodSet {
		resolution.importer.shortfalls.MergedTypeSets++
		record.made.Complete()
		return
	}
	explicit := map[*types.Func]*types.Func{}
	for index := range record.source.NumExplicitMethods() {
		explicit[record.source.ExplicitMethod(index)] = record.made.ExplicitMethod(index)
	}
	typeSet := make([]*types.Func, len(methods))
	for index, method := range methods {
		if made := explicit[method]; made != nil {
			typeSet[index] = made
			continue
		}
		typeSet[index] = resolution.translateInterfaceMethod(record.build, method)
	}
	types.CompleteWithMethods(record.made, typeSet)
}

// translateNamed is build's named type as the merged universe has it: an instance by its origin and type arguments, a
// package-level type by its package and name, a function-local one by where it is declared.
func (resolution *resolution) translateNamed(build *build, named *types.Named) types.Type {
	if named.TypeArgs().Len() > 0 {
		return resolution.instantiate(build, resolution.translate(build, named.Origin()), named.TypeArgs())
	}
	typeName := named.Obj()
	if typeName.Pkg() == nil {
		return named // error and comparable, the universe's own
	}
	if typeName.Parent() == typeName.Pkg().Scope() {
		object := resolution.object(typeName.Pkg(), typeName.Name())
		if object == nil {
			// Invariant: a build's package-level type name is declared in that build, so its merge finds it there.
			panic(fmt.Sprintf("merged: %s.%s is in no build's scope", typeName.Pkg().Path(), typeName.Name()))
		}
		return object.Type() // the invalid type when no build could read it
	}
	key := localKey{path: typeName.Pkg().Path(), name: typeName.Name(),
		position: resolution.importer.fileSet.Position(typeName.Pos()).String()}
	record := resolution.importer.locals[key]
	if record == nil {
		record = resolution.startNamed(
			types.NewTypeName(typeName.Pos(), resolution.importer.mergedPackageOf(typeName.Pkg()),
				typeName.Name(), nil),
			0,
		)
		resolution.importer.locals[key] = record
		build.translated[named] = record.named
		resolution.bindNamedParameters(record, build, named)
	} else {
		build.translated[named] = record.named
		resolution.shareParameters(build, resolution.typeParameters(build, named), record.named.TypeParams())
	}
	record.builds |= build.mask
	resolution.fillNamed(record, build, named)
	return record.named
}

// translateAlias is build's alias as the merged universe has it: an instance by its origin and type arguments, a
// package-level alias by its package and name, and a function-local one as its target.
func (resolution *resolution) translateAlias(build *build, alias *types.Alias) types.Type {
	if alias.TypeArgs().Len() > 0 {
		return resolution.instantiate(build, resolution.translate(build, alias.Origin()), alias.TypeArgs())
	}
	typeName := alias.Obj()
	if typeName.Pkg() == nil {
		return alias // any, the universe's own
	}
	if typeName.Parent() != typeName.Pkg().Scope() {
		return resolution.translate(build, alias.Rhs())
	}
	object := resolution.object(typeName.Pkg(), typeName.Name())
	merged, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		if object != nil {
			return object.Type() // no build could read it: the invalid type
		}
		// Invariant: a build's package-level alias is declared in that build, so its merge finds it there.
		panic(fmt.Sprintf("merged: %s.%s is in no build's scope", typeName.Pkg().Path(), typeName.Name()))
	}
	record := resolution.importer.aliases[merged]
	if record == nil {
		// The name is a defined type in the first build that declares it, a kind conflict counted there.
		return merged.Type()
	}
	if made := record.byBuild[build.index]; made != nil {
		return made
	}
	// The alias's own declaration is still being read: its target stands in.
	return resolution.translate(build, alias.Rhs())
}

// instantiate is origin, a merged generic type, instantiated with build's type arguments, shared across builds through
// the importer's context.
func (resolution *resolution) instantiate(build *build, origin types.Type, arguments *types.TypeList) types.Type {
	translated := make([]types.Type, arguments.Len())
	for index := range translated {
		translated[index] = resolution.translate(build, arguments.At(index))
	}
	instance, instantiateError := types.Instantiate(resolution.importer.context, origin, translated, false)
	if instantiateError != nil {
		// Invariant: without validation Instantiate reports no error. Only a count of arguments that differs from
		// the parameters' panics, and the merged origin has the declaration's own.
		panic(fmt.Sprintf("merged: instantiating %s: %v", origin, instantiateError))
	}
	return instance
}
