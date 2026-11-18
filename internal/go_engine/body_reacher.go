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
type bodyReacher struct {
	host       *bodyReachHost
	dependence *build_dependence.Dependence
	decoded    *build_dependence.Counterparts
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
				unitDependent: true})
			continue
		}
		position := tokenFile.Position(declared.Declaration.File + token.Pos(declared.Declaration.Offset))
		if !read.files[position.Filename] {
			absent++
			continue
		}
		bodies = append(bodies, reachedBody{packageIdentifier: hosted.ID, file: position.Filename,
			line: position.Line, name: name,
			reason: reacher.reason(hosted.Types, record, declared, read.buildOnlyNames,
				read.dotImports[position.Filename]),
			unitDependent: reacher.dependence.UnitDependent(hosted.ID, position.Filename,
				declared.Declaration.Offset)})
	}
	return bodies, absent
}

// reason is why declared's body is reached in the build, empty when it is not, own being its package's types,
// shadowing the names the package's build-only files declare at package level, and dotImported the paths its file
// imports by a dot.
func (reacher *bodyReacher) reason(
	own *types.Package,
	record *build_reads.FunctionUses,
	declared build_reads.DeclarationUses,
	shadowing map[string]bool,
	dotImported []string,
) bodyReachReason {
	if declared.SignatureUnknown || declared.BodyUnknown {
		return reachedUnknown
	}
	if reacher.partReason(record, declared.Signature) != "" {
		return reachedSignature
	}
	if reason := reacher.partReason(record, declared.Body); reason != "" {
		return reason
	}
	if len(shadowing) > 0 && (shadowed(own, record, declared.Signature, shadowing, dotImported) ||
		shadowed(own, record, declared.Body, shadowing, dotImported)) {
		return reachedShadowed
	}
	return ""
}

// partReason is the kind of the first of uses, in the record's order, that reaches a set bit in the build, empty when
// none does.
func (reacher *bodyReacher) partReason(record *build_reads.FunctionUses, uses []build_reads.PartUse) bodyReachReason {
	for _, use := range uses {
		if reason := reacher.useReason(record.Used(use), use.Kinds()); reason != "" {
			return reason
		}
	}
	return ""
}

// useReason is the kind by which entry, used by kinds, reaches a set bit in the build, empty when it reaches none
// (the mapping body_reach_host.go writes out). A universe object has no bit, the same in every build but where a
// build-only file shadows its name (shadowed), and an import's name none, being its file's own, which is the same file
// in both builds or no shared body's.
func (reacher *bodyReacher) useReason(entry build_reads.UsedEntry, kinds build_reads.UseKinds) bodyReachReason {
	if entry.Index != "" {
		if entry.Object.Pkg() != nil && reacher.bits(entry.Object).Name {
			return reachedSelected
		}
		if entry.Receiver != nil && entry.Receiver.Pkg() != nil && reacher.bits(entry.Receiver).Underlying {
			return reachedSelected
		}
		return ""
	}
	if entry.Object.Pkg() == nil {
		return ""
	}
	if _, isImport := entry.Object.(*types.PkgName); isImport {
		return ""
	}
	bits := reacher.bits(entry.Object)
	switch {
	case kinds&build_reads.UsedNamed != 0 && bits.Name && kinds&build_reads.UsedConstant != 0:
		return reachedConstant
	case kinds&build_reads.UsedNamed != 0 && bits.Name:
		return reachedNamed
	case kinds&build_reads.UsedValueType != 0 && (bits.Name || bits.Underlying):
		return reachedValueType
	case kinds&build_reads.UsedTypeArgument != 0 && (bits.Name || bits.Underlying):
		return reachedTypeArgument
	}
	return ""
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

// shadowed reports whether any of uses names, by an identifier, an object of the universe or of a package its file
// imports by a dot, dotImported, whose name shadowing holds, a name a build-only file of own declares at package
// level, so the name means that file's object in the build rather than the one the host recorded.
func shadowed(
	own *types.Package,
	record *build_reads.FunctionUses,
	uses []build_reads.PartUse,
	shadowing map[string]bool,
	dotImported []string,
) bool {
	for _, use := range uses {
		entry := record.Used(use)
		if entry.Index != "" || !shadowing[entry.Object.Name()] {
			continue
		}
		if entry.Object.Pkg() == nil {
			return true
		}
		if _, isImport := entry.Object.(*types.PkgName); isImport {
			continue
		}
		if use.Kinds()&build_reads.UsedNamed != 0 && entry.Object.Pkg() != own &&
			slices.Contains(dotImported, entry.Object.Pkg().Path()) {
			return true
		}
	}
	return false
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
