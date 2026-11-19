package go_engine

import (
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// bodyReacher judges one build's bodies on one goroutine, the build's own: dependence is the build's judgment, whose
// ObjectBits is safe from one goroutine at a time, and decoded answers, by its package's ID and its name, an object of
// a package the host's graph took from a record, which holds no live object for ObjectBits to find, made at the first
// such object.
//
// identities render the identity of the object that reaches each body first, its driver (reachedBody.driver), by the
// host's file set, made at the first.
type bodyReacher struct {
	host       *bodyReachHost
	dependence *build_dependence.Dependence
	decoded    *build_dependence.IdentifiedObjects
	identities *build_reads.IdentityRenderer
	fileSet    *token.FileSet
	// graph is what the build's whole graph holds (reachBuiltGraph), against which the load mode rule reads how the
	// host loaded each package, and constantsOnly has it reach by an untyped float constant alone, for the census's
	// count of what the general rule costs (loadModeReaches).
	graph         *reachBuiltGraph
	constantsOnly bool
	// sizesDiffer is whether the build's sizes are not the host's, which reaches every body (buildSizesDiffer).
	sizesDiffer bool
	// carried are the objects declared by a value spec that reads what the load mode reaches, which hold the other
	// load's value and so reach their users by the load mode too (carryLoadMode), and carriedGrew whether a judgment
	// carried one it had not, which judges the build again (bodyReachHost.reach).
	carried     map[types.Object]bool
	carriedGrew bool
	// someLoadModeDiffers is whether any package the host holds starts the carry of an unrecorded package's values
	// (startsCarry, carryUnrecorded), read at the first such package or as the census ends, and someLoadModeRead
	// whether it has been. lackedImported are the IDs of the host's packages that start it by the check alone, the
	// build holding nothing of their path and a file it compiles importing it all the same, read with it.
	someLoadModeDiffers bool
	someLoadModeRead    bool
	lackedImported      map[string]bool
}

// reachPackage judges every body of hosted's record that the build compiles, read saying how it compiles the package,
// and counts those in files it does not, absent. Whether the build compiles a body's file is read by the file as
// parsed, never as a //line directive renames it, which names no file the build lists (#w72zxtq, the review's must 8);
// the renamed file is the one the census reports and the graph's units are placed in.
func (reacher *bodyReacher) reachPackage(
	hosted *packages.Package,
	record *build_reads.FunctionUses,
	read *reachBuiltPackage,
) ([]reachedBody, int) {
	var bodies []reachedBody
	absent := 0
	for declared := range record.Declarations() {
		if !declared.HasBody {
			continue
		}
		name := declarationName(declared.Declaration)
		tokenFile := hosted.Fset.File(declared.Declaration.File)
		if tokenFile == nil {
			// A record always places its declarations in the host's files; one that does not is called reached, as
			// a doubt is, and dependent, as the graph calls a place it knows no unit at.
			bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, name: name, reason: reachedUnknown,
				partReason: reachedUnknown, constantsOnlyReason: reachedUnknown, unitDependent: true})
			continue
		}
		if !read.files[tokenFile.Name()] {
			absent++
			continue
		}
		position := tokenFile.Position(declared.Declaration.File + token.Pos(declared.Declaration.Offset))
		if reacher.sizesDiffer {
			bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, file: position.Filename,
				line: position.Line, offset: declared.Declaration.Offset, name: name, reason: reachedSizes,
				partReason: reachedSizes, driver: "the build's sizes", constantsOnlyReason: reachedSizes,
				unitDependent: reacher.dependence.UnitDependent(hosted.ID, position.Filename,
					declared.Declaration.Offset)})
			continue
		}
		if reacher.identities == nil || reacher.fileSet != hosted.Fset {
			reacher.identities, reacher.fileSet = build_reads.NewIdentityRenderer(hosted.Fset, nil), hosted.Fset
		}
		reason, driver, partReason := reacher.reason(hosted.Types, record, declared, read.buildOnlyNames,
			read.dotImports[tokenFile.Name()])
		// A body the general rule reaches is judged again under the constants-only load mode rule, the census's count
		// of what the general one costs: the load mode may have reached it through its signature as well as its body.
		// An unreached body is unreached by both, the narrower rule reaching nothing the general one does not.
		reacher.carryLoadMode(record, declared)
		constantsOnlyReason := reason
		if reason != "" {
			reacher.constantsOnly = true
			constantsOnlyReason, _, _ = reacher.reason(hosted.Types, record, declared, read.buildOnlyNames,
				read.dotImports[tokenFile.Name()])
			reacher.constantsOnly = false
		}
		bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, file: position.Filename,
			line: position.Line, offset: declared.Declaration.Offset, name: name, reason: reason,
			partReason: partReason, driver: driver, constantsOnlyReason: constantsOnlyReason,
			unitDependent: reacher.dependence.UnitDependent(hosted.ID, position.Filename,
				declared.Declaration.Offset)})
	}
	return bodies, absent
}

// sizesReason is Sizes when the build's sizes are not the host's, which reaches every body, and empty when they are.
func (reacher *bodyReacher) sizesReason() bodyReachReason {
	if reacher.sizesDiffer {
		return reachedSizes
	}
	return ""
}

// reason is why declared's body is reached in the build, empty when it is not, own being its package's types,
// shadowing the names the package's build-only files declare at package level, and dotImported the paths its file
// imports by a dot, and its driver: what reached it first, the object's identity, the kind of use and the bits it
// found set, or the unknown part or the shadowed name, which the census tallies. The last is the reason the part that
// reached it gave (reachedBody.partReason), which differs from the first for a signature alone: whatever reaches there
// is written Signature, the body type-checking against it, and the census counts those by the rule that found each.
func (reacher *bodyReacher) reason(
	own *types.Package,
	record *build_reads.FunctionUses,
	declared build_reads.DeclarationUses,
	shadowing map[string]bool,
	dotImported []string,
) (bodyReachReason, string, bodyReachReason) {
	switch {
	case declared.SignatureUnknown:
		return reachedUnknown, "an unknown identity in the signature", reachedUnknown
	case declared.BodyUnknown:
		return reachedUnknown, "an unknown identity in the body", reachedUnknown
	}
	if reason, driver := reacher.partReason(record, declared.Signature); reason != "" {
		return reachedSignature, driver + " in the signature", reason
	}
	if reason, driver := reacher.partReason(record, declared.Body); reason != "" {
		return reason, driver + " in the body", reason
	}
	if len(shadowing) == 0 {
		return "", "", ""
	}
	if name, isShadowed := shadowed(own, record, declared.Signature, shadowing, dotImported); isShadowed {
		return reachedShadowed, name + " shadowed in the signature", reachedShadowed
	}
	if name, isShadowed := shadowed(own, record, declared.Body, shadowing, dotImported); isShadowed {
		return reachedShadowed, name + " shadowed in the body", reachedShadowed
	}
	return "", "", ""
}

// partReason is the kind of the first of uses, in the record's order, that reaches a set bit in the build, empty when
// none does, and that use written as its driver: its object's identity, its kind and the bits set.
func (reacher *bodyReacher) partReason(
	record *build_reads.FunctionUses,
	uses []build_reads.PartUse,
) (bodyReachReason, string) {
	for _, use := range uses {
		if reason, object, bits := reacher.useReason(record.Used(use), use.Kinds()); reason != "" {
			return reason, reacher.identities.Object(object).String() + " " + string(reason) + " " + bits
		}
	}
	return "", ""
}

// useReason is the kind by which entry, used by kinds, reaches a set bit in the build, empty when it reaches none
// (the mapping body_reach_host.go writes out), with the object whose bit it found set and which bits those are. A
// universe object has no bit, the same in every build but where a build-only file shadows its name (shadowed), and an
// import's name none, being its file's own, which is the same file in both builds or no shared body's.
func (reacher *bodyReacher) useReason(
	entry build_reads.UsedEntry,
	kinds build_reads.UseKinds,
) (bodyReachReason, types.Object, string) {
	if entry.Index != "" {
		if entry.Object.Pkg() != nil && reacher.bits(entry.Object).Name {
			return reachedSelected, entry.Object, "name"
		}
		if entry.Receiver != nil && entry.Receiver.Pkg() != nil && reacher.bits(entry.Receiver).Underlying {
			return reachedSelected, entry.Receiver, "receiver underlying"
		}
		if entry.Object.Pkg() != nil && reacher.loadModeReaches(entry.Object, kinds) {
			return reachedLoadMode, entry.Object, "load mode"
		}
		if entry.Receiver != nil && entry.Receiver.Pkg() != nil && reacher.loadModeReaches(entry.Receiver, kinds) {
			return reachedLoadMode, entry.Receiver, "load mode"
		}
		return "", nil, ""
	}
	if entry.Object.Pkg() == nil {
		return "", nil, ""
	}
	if _, isImport := entry.Object.(*types.PkgName); isImport {
		return "", nil, ""
	}
	bits := reacher.bits(entry.Object)
	switch {
	case kinds&build_reads.UsedNamed != 0 && bits.Name && kinds&build_reads.UsedConstant != 0:
		return reachedConstant, entry.Object, "name"
	case kinds&build_reads.UsedNamed != 0 && bits.Name:
		return reachedNamed, entry.Object, "name"
	case kinds&build_reads.UsedValueType != 0 && (bits.Name || bits.Underlying):
		return reachedValueType, entry.Object, writtenBits(bits)
	case kinds&build_reads.UsedTypeArgument != 0 && (bits.Name || bits.Underlying):
		return reachedTypeArgument, entry.Object, writtenBits(bits)
	case reacher.loadModeReaches(entry.Object, kinds):
		return reachedLoadMode, entry.Object, "load mode"
	}
	return "", nil, ""
}

// carryLoadMode carries the load mode through declared when it is a value spec that reads anything the load mode
// reaches (#w72zxtq, the review's must 3): `const twoPi = 2 * math.Pi` holds the other load's value of math.Pi as
// surely as the expression does, so every object it declares reaches its users by the load mode as well, the users of
// those it declares in turn, to a fixed point over the build (bodyReachHost.reach). An import's name carries nothing,
// as useReason reads none.
func (reacher *bodyReacher) carryLoadMode(record *build_reads.FunctionUses, declared build_reads.DeclarationUses) {
	if len(declared.Declaration.Objects) == 0 {
		return
	}
	if _, isFunction := declared.Declaration.Objects[0].(*types.Func); isFunction {
		return
	}
	reads := false
	for _, use := range slices.Concat(declared.Signature, declared.Body) {
		entry := record.Used(use)
		if _, isImport := entry.Object.(*types.PkgName); isImport {
			continue
		}
		if entry.Object.Pkg() != nil && reacher.loadModeReaches(entry.Object, use.Kinds()) ||
			entry.Receiver != nil && entry.Receiver.Pkg() != nil &&
				reacher.loadModeReaches(entry.Receiver, use.Kinds()) {
			reads = true
			break
		}
	}
	if !reads {
		return
	}
	for _, object := range declared.Declaration.Objects {
		reacher.carry(object)
	}
}

// carryUnrecorded carries the load mode through every package-level const and var of own, the types of a package the
// host recorded no uses of, when any package the host holds starts the carry in the build (startsCarry; #w72zxtq, the
// review's must A). The reach judges none of such a package's declarations, reaching its bodies whole, so it cannot
// tell which of its value specs read what the load mode reaches, as carryLoadMode tells of a recorded one's: `const
// Scale = math.Pi / 180` in a package checked for its types alone holds the other load's value, and every use of Scale
// elsewhere reaches. Carrying each is wider than needed, costing re-checks and never a body left unreached. A build
// that loads every package it holds as the host loaded it carries nothing, since no value can then hold another
// load's, whatever the host holds beside them.
func (reacher *bodyReacher) carryUnrecorded(own *types.Package) {
	if own == nil || !reacher.anyLoadModeDiffers() {
		return
	}
	scope := own.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Const, *types.Var:
			reacher.carry(object)
		}
	}
}

// carry marks object carried, and the judgment as having carried one it had not when it is new (carriedGrew).
func (reacher *bodyReacher) carry(object types.Object) {
	if !reacher.carried[object] {
		reacher.carried[object] = true
		reacher.carriedGrew = true
	}
}

// anyLoadModeDiffers reports whether any package the host holds starts the carry in the build (startsCarry), by every
// ID of the host's graph and of its reads, read once a build. A value can hold another load's only through a package
// loaded in another mode, and every package an unrecorded package's values read is one the host holds, having checked
// it.
func (reacher *bodyReacher) anyLoadModeDiffers() bool {
	reacher.readCarryStarts()
	return reacher.someLoadModeDiffers
}

// lackedYetImported are the IDs of the host's packages that start the carry by its check alone (startsCarry), sorted:
// none, in every run the argument holds of.
func (reacher *bodyReacher) lackedYetImported() []string {
	reacher.readCarryStarts()
	return slices.Sorted(maps.Keys(reacher.lackedImported))
}

// readCarryStarts asks startsCarry of every package the host holds, once a build: of every ID of its graph, with the
// path the graph holds it by, and of every ID of its reads, with the package's own.
func (reacher *bodyReacher) readCarryStarts() {
	if reacher.someLoadModeRead {
		return
	}
	reacher.someLoadModeRead = true
	reacher.lackedImported = map[string]bool{}
	read := func(identifier string, path string) {
		starts, byImport := reacher.startsCarry(identifier, path)
		reacher.someLoadModeDiffers = reacher.someLoadModeDiffers || starts
		if byImport {
			reacher.lackedImported[identifier] = true
		}
	}
	for identifier, path := range reacher.host.dependence.paths {
		read(identifier, path)
	}
	for identifier, hosted := range reacher.host.packages {
		read(identifier, hosted.PkgPath)
	}
}

// startsCarry reports whether the package the host holds by identifier, of path, starts the carry of an unrecorded
// package's values in the build (carryUnrecorded), and whether it does so by the check alone (#wzre5eg). The carry
// exists because such a value may have been computed from a package the two sides loaded in different modes, so the
// build's graph is read for the package's path, in three cases:
//   - The build holds its ID: the two loads are compared, as the direct rule compares them (identifiedLoadModeDiffers).
//   - The build lacks its ID and holds its path under another: every package it holds of the path stands in, and the
//     carry starts unless each is loaded in the host's mode for the package.
//   - The build holds nothing of its path: it starts no carry. A file both builds compile has the same imports in
//     both, so nothing the build compiles imports a package its graph lacks, and no value there was computed from one.
//
// The last case's argument is held as a check, never taken on trust: where a file of a package the build checked from
// source imports the path all the same, by its import spec, read apart from the graph (reachBuiltGraph.imports), the
// carry starts as it did before the case was told apart, and the census counts the package on a line of its own, 0 in
// every run the argument holds of.
//
// In practice the second case is every command with an in-package test that nothing imports, 31 of cohere's own: an
// extra build's load leaves such a root p out (load.Caching's AnalyzedRootsOnly) and checks its test variant
// "p [p.test]", which holds every file of p, in its place, so the host's p is held against that variant. The third is
// a package only the host's platform compiles, as internal/syscall/unix is to a windows build. Read by its ID alone,
// either was a package the host checked from source and the build did not, loaded in another mode, and carried every
// value of every unrecorded package in every run, 9,624 bodies reached at cohere's root that nothing differed for.
func (reacher *bodyReacher) startsCarry(identifier string, path string) (starts bool, byImport bool) {
	if reacher.graph.holds(identifier) {
		return reacher.identifiedLoadModeDiffers(identifier), false
	}
	standIns := reacher.graph.identifiers[path]
	if len(standIns) == 0 {
		imported := reacher.graph.imports(path)
		return imported, imported
	}
	for _, standIn := range standIns {
		if reacher.host.dependence.exportData[identifier] == reacher.graph.checkedFromSource(standIn) {
			return true, false
		}
	}
	return false, false
}

// loadModeReaches reports whether a use of kinds of object reaches its body by the load mode (the soundness contract's
// point 5, #9paaen9): object's package is one the host and the build load in different modes, one from export data
// and the other from source, which expose the same files differently. Export data holds an untyped float as a binary
// fraction of 512 bits, and a check from source holds it exactly, so goja's math.Sqrt2 / 2 type-checks to two values at
// cohere's root wherever the host reads math from the system cache and a build checks it from source, though no
// declaration differs. A build reads the system cache as the host does, under `--build-dependence` too (#wzre5eg), so
// the two load the standard library alike but where one of them misses. The Square ruled the rule general (#w72zxtq):
// every use of any object of such a package reaches. With constantsOnly, the census's count of the narrower rule it
// replaced, only an untyped float or complex constant read by its value does, since an integer, string or boolean
// constant is written exactly in both and a typed float is rounded to its type in both. A package the host does not
// hold differs. An object a value spec declared from what the load mode reaches reaches it as well, under either rule
// (carryLoadMode): the constants-only rule reads the general rule's carried objects, of them the untyped float and
// complex constants alone.
func (reacher *bodyReacher) loadModeReaches(object types.Object, kinds build_reads.UseKinds) bool {
	if reacher.constantsOnly {
		constant, isConstant := object.(*types.Const)
		if !isConstant || kinds&build_reads.UsedConstant == 0 || !isUntypedFloatingPoint(constant.Type()) {
			return false
		}
	}
	return reacher.carried[object] || reacher.loadModeDiffers(object.Pkg())
}

// loadModeDiffers reports whether the host and the build load own in different modes. A package the host does not
// hold has no mode to compare, and so differs, failing wide (#w72zxtq). This is the direct rule, of a body that names
// an object of own, and it reads a package by its ID alone: one the host checked from source and the build's graph
// lacks differs here, though it starts no carry (startsCarry).
func (reacher *bodyReacher) loadModeDiffers(own *types.Package) bool {
	identifier, isHeld := reacher.host.identifiers[own]
	if !isHeld {
		return true
	}
	return reacher.identifiedLoadModeDiffers(identifier)
}

// identifiedLoadModeDiffers reports whether the host and the build load the package the host holds by identifier in
// different modes: one from export data and the other from source.
func (reacher *bodyReacher) identifiedLoadModeDiffers(identifier string) bool {
	return reacher.host.dependence.exportData[identifier] == reacher.graph.checkedFromSource(identifier)
}

// isUntypedFloatingPoint reports whether typed is the untyped float or untyped complex type, whose constants hold
// values past any machine type's.
func isUntypedFloatingPoint(typed types.Type) bool {
	basic, isBasic := typed.(*types.Basic)
	return isBasic && (basic.Kind() == types.UntypedFloat || basic.Kind() == types.UntypedComplex)
}

// writtenBits are the bits set of bits, as the census's drivers write them.
func writtenBits(bits build_dependence.Bits) string {
	switch {
	case bits.Name && bits.Underlying:
		return "name and underlying"
	case bits.Name:
		return "name"
	}
	return "underlying"
}

// bits are object's bits in the build: by the build's judgment of the host's graph, or, for an object of a package the
// graph took from a findings store's record, by its package's ID and its name there
// (build_dependence.IdentifiedObjects), since such a record holds no live object for ObjectBits to find and would read
// every one as unchanged. A name is never asked by its path, which a package and its test variants share.
func (reacher *bodyReacher) bits(object types.Object) build_dependence.Bits {
	if !reacher.host.dependence.decoded[object.Pkg()] {
		return reacher.dependence.ObjectBits(object)
	}
	if reacher.decoded == nil {
		reacher.decoded = build_dependence.NewIdentifiedObjects(reacher.dependence, reacher.host.identifiers)
	}
	return reacher.decoded.Bits(object)
}

// shadowed is the first name any of uses names, by an identifier, of an object of the universe or of a package its
// file imports by a dot, dotImported, that shadowing holds, a name a build-only file of own declares at package level,
// so the name means that file's object in the build rather than the one the host recorded, and whether there is one.
func shadowed(
	own *types.Package,
	record *build_reads.FunctionUses,
	uses []build_reads.PartUse,
	shadowing map[string]bool,
	dotImported []string,
) (string, bool) {
	for _, use := range uses {
		entry := record.Used(use)
		if entry.Index != "" || !shadowing[entry.Object.Name()] {
			continue
		}
		if entry.Object.Pkg() == nil {
			return "universe." + entry.Object.Name(), true
		}
		if _, isImport := entry.Object.(*types.PkgName); isImport {
			continue
		}
		if use.Kinds()&build_reads.UsedNamed != 0 && entry.Object.Pkg() != own &&
			slices.Contains(dotImported, entry.Object.Pkg().Path()) {
			return entry.Object.Pkg().Path() + "." + entry.Object.Name(), true
		}
	}
	return "", false
}

// declarationName is declaration's name as the census lists it: a method's as its receiver's type and its own, a
// value spec's as its names, and an init function or a blank value by the name it is declared by.
func declarationName(declaration build_reads.Declaration) string {
	if len(declaration.Objects) == 0 {
		return declaration.Name
	}
	if function, isFunction := declaration.Objects[0].(*types.Func); isFunction {
		if receiver := function.Signature().Recv(); receiver != nil {
			if named := namedReceiver(receiver.Type()); named != nil {
				return named.Obj().Name() + "." + function.Name()
			}
		}
		return function.Name()
	}
	names := make([]string, 0, len(declaration.Objects))
	for _, object := range declaration.Objects {
		names = append(names, object.Name())
	}
	return strings.Join(names, ", ")
}

// namedReceiver is the named type a method's receiver type is or points to, nil for any other.
func namedReceiver(receiver types.Type) *types.Named {
	pointedTo := types.Unalias(receiver)
	if pointer, isPointer := pointedTo.(*types.Pointer); isPointer {
		pointedTo = types.Unalias(pointer.Elem())
	}
	named, isNamed := pointedTo.(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}
