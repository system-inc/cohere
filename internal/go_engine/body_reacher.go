package go_engine

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// bodyReacher judges one build's bodies on one goroutine, the build's own: dependence is the build's judgment, whose
// ObjectBits is safe from one goroutine at a time, and decoded answers, by name, an object of a package the host's
// graph took from a record, which holds no live object for ObjectBits to find, made at the first such object.
//
// identities render the identity of the object that reaches each body first, its driver (reachedBody.driver), by the
// host's file set, made at the first.
type bodyReacher struct {
	host       *bodyReachHost
	dependence *build_dependence.Dependence
	decoded    *build_dependence.Counterparts
	identities *build_reads.IdentityRenderer
	fileSet    *token.FileSet
	// fromSource are the IDs of the packages the build checked from source, against which the load mode rule reads
	// how the host loaded each, and constantsOnly has it reach by an untyped float constant alone, for the census's
	// count of what the general rule costs (loadModeReaches).
	fromSource    map[string]bool
	constantsOnly bool
}

// reachPackage judges every body of hosted's record that the build compiles, read saying how it compiles the package,
// and counts those in files it does not, absent.
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
				constantsOnlyReason: reachedUnknown, unitDependent: true})
			continue
		}
		position := tokenFile.Position(declared.Declaration.File + token.Pos(declared.Declaration.Offset))
		if !read.files[position.Filename] {
			absent++
			continue
		}
		if reacher.identities == nil || reacher.fileSet != hosted.Fset {
			reacher.identities, reacher.fileSet = build_reads.NewIdentityRenderer(hosted.Fset, nil), hosted.Fset
		}
		reason, driver := reacher.reason(hosted.Types, record, declared, read.buildOnlyNames,
			read.dotImports[position.Filename])
		// A body the general rule reaches is judged again under the constants-only load mode rule, the census's count
		// of what the general one costs: the load mode may have reached it through its signature as well as its body.
		// An unreached body is unreached by both, the narrower rule reaching nothing the general one does not.
		constantsOnlyReason := reason
		if reason != "" {
			reacher.constantsOnly = true
			constantsOnlyReason, _ = reacher.reason(hosted.Types, record, declared, read.buildOnlyNames,
				read.dotImports[position.Filename])
			reacher.constantsOnly = false
		}
		bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, file: position.Filename,
			line: position.Line, offset: declared.Declaration.Offset, name: name, reason: reason, driver: driver,
			constantsOnlyReason: constantsOnlyReason,
			unitDependent: reacher.dependence.UnitDependent(hosted.ID, position.Filename,
				declared.Declaration.Offset)})
	}
	return bodies, absent
}

// reason is why declared's body is reached in the build, empty when it is not, own being its package's types,
// shadowing the names the package's build-only files declare at package level, and dotImported the paths its file
// imports by a dot, and its driver: what reached it first, the object's identity, the kind of use and the bits it
// found set, or the unknown part or the shadowed name, which the census tallies.
func (reacher *bodyReacher) reason(
	own *types.Package,
	record *build_reads.FunctionUses,
	declared build_reads.DeclarationUses,
	shadowing map[string]bool,
	dotImported []string,
) (bodyReachReason, string) {
	switch {
	case declared.SignatureUnknown:
		return reachedUnknown, "an unknown identity in the signature"
	case declared.BodyUnknown:
		return reachedUnknown, "an unknown identity in the body"
	}
	if reason, driver := reacher.partReason(record, declared.Signature); reason != "" {
		return reachedSignature, driver + " in the signature"
	}
	if reason, driver := reacher.partReason(record, declared.Body); reason != "" {
		return reason, driver + " in the body"
	}
	if len(shadowing) == 0 {
		return "", ""
	}
	if name, isShadowed := shadowed(own, record, declared.Signature, shadowing, dotImported); isShadowed {
		return reachedShadowed, name + " shadowed in the signature"
	}
	if name, isShadowed := shadowed(own, record, declared.Body, shadowing, dotImported); isShadowed {
		return reachedShadowed, name + " shadowed in the body"
	}
	return "", ""
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

// loadModeReaches reports whether a use of kinds of object reaches its body by the load mode (the soundness contract's
// point 5, #9paaen9): object's package is one the host and the build load in different modes, one from export data
// and the other from source, which expose the same files differently. Export data holds an untyped float as a binary
// fraction of 512 bits, and a check from source holds it exactly, so goja's math.Sqrt2 / 2 type-checks to two values at
// cohere's root, where the host reads math from the system cache and every build under `--build-dependence` checks it
// from source, though no declaration differs. The Square ruled the rule general (#w72zxtq): every use of any object
// of such a package reaches. With constantsOnly, the census's count of the narrower rule it replaced, only an untyped
// float or complex constant read by its value does, since an integer, string or boolean constant is written exactly in
// both and a typed float is rounded to its type in both. A package the host does not hold is never asked.
func (reacher *bodyReacher) loadModeReaches(object types.Object, kinds build_reads.UseKinds) bool {
	if reacher.constantsOnly {
		constant, isConstant := object.(*types.Const)
		if !isConstant || kinds&build_reads.UsedConstant == 0 || !isUntypedFloatingPoint(constant.Type()) {
			return false
		}
	}
	return reacher.loadModeDiffers(object.Pkg())
}

// loadModeDiffers reports whether the host and the build load own in different modes.
func (reacher *bodyReacher) loadModeDiffers(own *types.Package) bool {
	identifier, isHeld := reacher.host.identifiers[own]
	if !isHeld {
		return false
	}
	return reacher.host.dependence.exportData[identifier] == reacher.fromSource[identifier]
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
// graph took from a findings store's record, by its name there (build_dependence.Counterparts), since such a record
// holds no live object for ObjectBits to find and would read every one as unchanged.
func (reacher *bodyReacher) bits(object types.Object) build_dependence.Bits {
	if !reacher.host.dependence.decoded[object.Pkg()] {
		return reacher.dependence.ObjectBits(object)
	}
	if reacher.decoded == nil {
		reacher.decoded = build_dependence.NewCounterparts(reacher.host.dependence.graph, reacher.dependence)
	}
	name, underlying := reacher.decoded.DependentObject(object)
	return build_dependence.Bits{Name: name, Underlying: underlying}
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
