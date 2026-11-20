package go_engine

import (
	"cmp"
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
// the renamed file is the one the census reports and the graph's units are placed in. A package whose imports differ
// is then reached whole (differingImports), and the package's variable initializers are judged as one body, by every
// body of the package and every name one side alone declares (reachInitializers).
func (reacher *bodyReacher) reachPackage(
	hosted *packages.Package,
	record *build_reads.FunctionUses,
	read *reachBuiltPackage,
) ([]reachedBody, int) {
	var bodies []reachedBody
	absent := 0
	// initializers are the indexes in bodies of the package's variable initializers, and unshared what one side alone
	// holds of the package, which reaches every one of them whatever any uses (reachInitializers), the first of: an
	// initializer in a file the builds do not share, a host's file the build does not compile or a build-only file,
	// then a body in either, then a name either declares.
	var initializers []int
	unshared := ""
	absentInitializer, hostOnlyName := false, false
	for declared := range record.Declarations() {
		if !declared.HasBody {
			// A type, a value with no initializer or a function declared without a body, in a file the build does
			// not compile, is the host's side of a name one side alone declares, as buildOnlyNames is the build's. A
			// record that places it in no file of the host's is a doubt, and taken as one.
			if !hostOnlyName {
				tokenFile := hosted.Fset.File(declared.Declaration.File)
				hostOnlyName = tokenFile == nil || !read.files[tokenFile.Name()]
			}
			continue
		}
		name := declarationName(declared.Declaration)
		isInitializer := isVariableInitializer(declared.Declaration)
		tokenFile := hosted.Fset.File(declared.Declaration.File)
		if tokenFile == nil {
			if isInitializer {
				initializers = append(initializers, len(bodies))
			}
			// A record always places its declarations in the host's files; one that does not is called reached, as
			// a doubt is, and dependent, as the graph calls a place it knows no unit at.
			bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, name: name, reason: reachedUnknown,
				partReason: reachedUnknown, constantsOnlyReason: reachedUnknown, unitDependent: true})
			continue
		}
		if !read.files[tokenFile.Name()] {
			// An absent initializer is no body of the build's, only the cause that reaches every one it shares.
			absentInitializer = absentInitializer || isInitializer
			absent++
			continue
		}
		if isInitializer {
			initializers = append(initializers, len(bodies))
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
		// A body a //line directive moves onto a file the builds do not share is judged by that file, whatever it uses
		// (unsharedLineClaims).
		if claimed, isClaimed := read.lineClaims[tokenFile.Name()][declared.Declaration.Offset]; isClaimed {
			if reason == "" {
				reason, driver = reachedLineClaim, claimed+" claimed by a //line directive"
			}
			constantsOnlyReason = cmp.Or(constantsOnlyReason, reachedLineClaim)
		}
		bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, file: position.Filename,
			line: position.Line, offset: declared.Declaration.Offset, name: name, reason: reason,
			partReason: partReason, driver: driver, constantsOnlyReason: constantsOnlyReason,
			unitDependent: reacher.dependence.UnitDependent(hosted.ID, position.Filename,
				declared.Declaration.Offset)})
	}
	differing := differingImports(hosted.Imports, read.imports)
	// A package's reads hold its lookups, as its function uses (bodyReachHost.judge).
	if reads := reacher.host.reads[hosted]; reads != nil {
		differing = append(differing, differingClosureLookups(reads.Lookups(), hosted, read)...)
	}
	if len(differing) > 0 {
		driver := "the package's imports differing, by " + strings.Join(differing, ", ")
		for index := range bodies {
			if bodies[index].reason == "" {
				bodies[index].reason, bodies[index].driver = reachedImports, driver
			}
			// The constants-only rule's count reads the general one's beside it, and this reason is either's alike.
			bodies[index].constantsOnlyReason = cmp.Or(bodies[index].constantsOnlyReason, reachedImports)
		}
	}
	switch {
	case read.buildOnlyInitializers || absentInitializer:
		unshared = "one in a file the builds do not share"
	case absent > 0:
		unshared = "a body of the package in a file the build does not compile"
	case read.buildOnlyBodies > 0:
		unshared = "a body of the package in a file the build alone compiles"
	case len(read.buildOnlyNames) > 0:
		unshared = "a name declared by a file the build alone compiles"
	case hostOnlyName:
		unshared = "a name declared by a file the build does not compile"
	}
	reachInitializers(bodies, initializers, unshared)
	return bodies, absent
}

// differingImports are the imports of a package that differ between the builds, sorted by path, hosted the host's
// import graph of it and built the build's, blank imports among them, `unsafe` too, which the graph holds as any other
// import: the paths its files import in one build and not the other, and the paths both import that resolve in the
// build to a package of another ID than on the host, each of those written with both IDs.
//
// A rule may judge every body by the paths, whatever the body uses (#bed0mnk): unusedwrite stands down in a package
// that imports `unsafe`, SA1015 in one that imports cobra, a command's, and SA5008 accepts a repeated struct tag key
// in one that imports go-flags.
//
// A path that resolves to another ID is another package under the same name (#bed0mnk, the reader's must 2 on
// #wzre5eg). go test builds a package again against a test variant, `via [audit.test]` against `audit [audit.test]`,
// and a test file may declare a universe name the package's own files use, so the variant's constants are not the
// package's, nor are those of a package built against it. A body that reads one type-checks to other values where
// only the build holds the variant, though no file of its package differs and every ID the host holds the build
// holds, loaded alike: the reach reads an object by the host's ID of its package, and here the build's body reads
// another package's.
func differingImports(hosted map[string]*packages.Package, built map[string]*packages.Package) []string {
	var differing []string
	for path, hostImported := range hosted {
		builtImported, isBuilt := built[path]
		switch {
		case !isBuilt:
			differing = append(differing, path)
		case builtImported.ID != hostImported.ID:
			differing = append(differing, path+" resolving to "+hostImported.ID+" on the host and to "+
				builtImported.ID+" in the build")
		}
	}
	for path := range built {
		if _, isHosted := hosted[path]; !isHosted {
			differing = append(differing, path)
		}
	}
	slices.Sort(differing)
	return differing
}

// differingClosureLookups are the Closure lookups among lookups, the questions the host's rules asked of hosted's
// import closure (build_reads.QueryClosure), that the build answers otherwise, asked again of read's types of the
// package of hosted's ID, each written with what either side found (#bed0mnk, the Square's must on the three-piece
// head). correctness-no-caller-data-mutation exempts a Read's buffer only where io is in the closure, and
// correctness-require-handled-error a dropped Write of a digest only where hash is, gates no use of a body reads, and
// a dependency that imports io on one platform alone flips them where the package's own imports are the same.
//
// The question is the rule's own (build_reads.ClosureInterface), asked of the closure from the package itself, which
// holds the closure from its imports that correctness-no-caller-data-mutation asks: the two differ only at the
// package's own path, which is skipped, being the package itself in both builds, its declarations judged by the
// bodies and names of each side's files. A lookup differs where it is found on one side alone, either way round, or
// where both find it in a package of other IDs, read by the path in each side's import graph, which holds one package
// of a path. The host's answer is its recorded one, never asked again.
//
// The package grain's comparison (check's lookupHolds) reads more than these: the answer's object by its objectpath
// and its closure's digest, from a memo the census does not hold. The answer here is an interface of the standard
// library that no platform declares otherwise, so its package's ID stands for it. Only the two lookups are compared,
// never the whole closures, which differ for nearly every package: `os` alone imports a platform's own packages.
func differingClosureLookups(
	lookups []build_reads.Lookup,
	hosted *packages.Package,
	read *reachBuiltPackage,
) []string {
	var differing []string
	for _, lookup := range lookups {
		if lookup.Query != build_reads.QueryClosure || lookup.Path == hosted.PkgPath {
			continue
		}
		asked := lookup.Path + "." + lookup.Name + " in the import closure, "
		_, declared := build_reads.ClosureInterface([]*types.Package{read.types}, lookup.Path, lookup.Name)
		switch {
		case lookup.Found && declared == nil:
			differing = append(differing, asked+"found on the host and not in the build")
		case !lookup.Found && declared != nil:
			differing = append(differing, asked+"found in the build and not on the host")
		case lookup.Found && declared != nil:
			hostIdentifier, builtIdentifier := closureIdentifier(hosted.Imports, lookup.Path),
				closureIdentifier(read.imports, lookup.Path)
			if hostIdentifier != builtIdentifier {
				differing = append(differing, asked+"found in "+hostIdentifier+" on the host and in "+builtIdentifier+
					" in the build")
			}
		}
	}
	return differing
}

// closureIdentifier is the ID of the package of path in the import graph from imports, a package's imports by path,
// empty where the graph holds none.
func closureIdentifier(imports map[string]*packages.Package, path string) string {
	identifier := ""
	packages.Visit(slices.Collect(maps.Values(imports)), func(each *packages.Package) bool {
		if each.PkgPath == path {
			identifier = each.ID
		}
		// Past the package found, or once one is, nothing more is asked.
		return identifier == ""
	}, nil)
	return identifier
}

// reachInitializers judges a package's variable initializers, the bodies at initializers's indexes, as one body
// (#bed0mnk): Go lowers them into one synthetic function, in the order `types.Info.InitOrder` gives, dependencies
// first, declaration order among the independent and files in the load's order, so one that never returns leaves
// every one after it unreachable, and a rule reading the representation reports nothing on them, though nothing they
// use differs. Every one is reached, Initializers, where the package holds anything one side alone does, unshared
// saying what, empty for nothing, or any body of the package is reached, an initializer's or a function's.
//
// Why that is enough inside one package (the Square's musts 1 and 2): the order is computed in the package alone,
// from the order its files declare its variables in and from what its own initializers and functions refer to. So it
// changes only where one of those bodies is other, missing or new, which is a body reached, absent or build-only, or
// where a name one refers to resolves otherwise, which takes a declaration only one side holds. And the cut moves
// with a function of the package that stops returning on one side, which is such a body too, itself or one it calls.
// Across packages the order cannot change, and the cut can, by a callee's no-return fact: that is the facts' reach's.
//
// The constants-only load mode rule is judged by the same rule from its own reasons, so the census's count of what
// the general rule costs stays its own (loadModeReaches).
//
// Mutates: bodies gains the reason at each initializer left unreached
func reachInitializers(bodies []reachedBody, initializers []int, unshared string) {
	driver := ""
	if unshared != "" {
		driver = "the package's variable initializers, " + unshared
	}
	constantsOnlyReached := unshared != ""
	for _, body := range bodies {
		if driver == "" && body.reason != "" {
			driver = "the package's variable initializers, " + body.name + " reached"
		}
		constantsOnlyReached = constantsOnlyReached || body.constantsOnlyReason != ""
	}
	for _, index := range initializers {
		if driver != "" && bodies[index].reason == "" {
			bodies[index].reason, bodies[index].driver = reachedInitializers, driver
		}
		if constantsOnlyReached && bodies[index].constantsOnlyReason == "" {
			bodies[index].constantsOnlyReason = reachedInitializers
		}
	}
}

// isVariableInitializer reports whether declaration is a value spec the package's synthetic initializing function
// runs: a var's with a value. A spec of blank names alone declares no object to tell a var from a const by, so it is
// taken as one, which costs a re-check at most and never leaves an initializer unreached.
func isVariableInitializer(declaration build_reads.Declaration) bool {
	if declaration.Kind != build_reads.DeclaredValue {
		return false
	}
	if len(declaration.Objects) == 0 {
		return true
	}
	_, isVariable := declaration.Objects[0].(*types.Var)
	return isVariable
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
// "p [p.test]" in its place, so the host's p is held against that variant. The third is a package only the host's
// platform compiles, as internal/syscall/unix is to a windows build. Read by its ID alone, either was a package the
// host checked from source and the build did not, loaded in another mode, and carried every value of every unrecorded
// package in every run, 9,624 bodies reached at cohere's root that nothing differed for.
//
// The variant holds every file of p, which is true of its files and not of what they mean. It holds its in-package
// test files too, and one of those can declare at package level a name p's own files read from the universe, as
// `const true = false` does, so a constant of the variant need not be p's, nor one of any package go test builds again
// against the variant: a package built again for another's tests can differ from its plain self, with no load mode
// differing. The second case answers for the load mode alone, and nothing of the carry covers a body that reads a
// stand-in, the carry being of an unrecorded package's values. What reaches such a body is the direct rule's wide
// answer (loadModeDiffers), which calls a package the build lacks by its ID loaded otherwise, under a label that names
// the wrong cause, and the imports (differingImports). Two holes the reader left open are closed so (#bed0mnk):
//   - The host restored p, from its findings store say, and the build holds only its variant: read by the mode alone,
//     export data on the host and nothing checked from source in the build were loaded alike, and a body that names an
//     object of p went unreached. The direct rule now calls p lacked by its ID differing whatever the host's mode.
//   - The build holds every ID the host does, loaded alike, and a body's import resolves there to a package of another
//     ID, one built again for a test ("q [p.test]" for q): no rule asked which package of a path a body's package
//     imports. Its package's imports now differ, and reach its bodies.
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
// hold differs, and so does one the build holds nothing of by its ID (loadModeDiffers). An object a value spec
// declared from what the load mode reaches reaches it as well, under either rule (carryLoadMode): the constants-only
// rule reads the general rule's carried objects, of them the untyped float and complex constants alone.
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
// an object of own, and it reads a package by its ID alone: one the build's graph lacks by that ID differs here
// whatever the host's mode (#bed0mnk, the reader's must 1 on #wzre5eg), though it starts no carry (startsCarry).
//
// The two differ on purpose. The carry asks whether a value may have been computed from a package loaded in two
// modes, so it reads the path and lets the package of another ID the build holds of it stand in, a command's test
// variant holding every file of the command. The direct rule asks whether the object the body names is the one the
// host recorded, and a stand-in's need not be: a test variant's own files may declare a universe name the package's
// files use, so the variant's constant is another value where nothing was loaded otherwise. Read by the mode alone, a
// package the host restored and the build lacks agreed, export data on one side and no check from source on the other.
func (reacher *bodyReacher) loadModeDiffers(own *types.Package) bool {
	identifier, isHeld := reacher.host.identifiers[own]
	if !isHeld {
		return true
	}
	return !reacher.graph.holds(identifier) || reacher.identifiedLoadModeDiffers(identifier)
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
