package build_reads

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// typeReads are the type reads of one package as its walk collects them, on the walk's own goroutine, and publishes
// them to its Reads under one hold of the lock (see Reads.publishTypes).
type typeReads struct {
	uses        map[ObjectUse]bool
	selections  map[string]Selection
	closureOnly string
}

// add records a use of kind of object, when object is another package's than own, as that package declares it.
func (collected *typeReads) add(own *types.Package, object types.Object, kind UseKind) {
	if foreign, isForeign := foreignDeclared(object, own); isForeign {
		collected.uses[ObjectUse{Object: foreign, Kind: kind}] = true
	}
}

// collectTypeReads is every object of another package that info, own's type information, reaches, each with the kind
// of each use the package made of it (#mjdf7ah): each identifier's use, by what it names (a type name named, a member
// or function used), each selection through another package's named type, each embedded field on a selection's path,
// each implicit object, and, from where each expression of files sits, every use of a whole type, an underlying type
// or a method set the package made (walkUses). An object of an instance, a field or method of an instantiated type or
// an instantiated function, is collected as its generic declares it, which is what another build can find. An object
// of no package, the universe's, is the same in every build.
//
// An instance is read by its type arguments, each judged by what a constraint can ask of it (walkUses), as is a value
// of a type argument inferred and never named: `generic.Identity(platform.Get())` copies Get's result into Identity's
// parameter, a use of platform.Handle whole.
//
// Without the syntax or the expression types, no use can be told apart from another, and a package that names a
// symbol by go:linkname reaches past the type checker, so each of those is judged by every object's closure, as every
// read was before kinds.
func collectTypeReads(own *types.Package, info *types.Info, files []*ast.File) typeReads {
	collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[string]Selection{}}
	for _, used := range info.Uses {
		kind := UseMember
		if _, isTypeName := used.(*types.TypeName); isTypeName {
			kind = UseName
		}
		collected.add(own, used, kind)
	}
	for _, selection := range info.Selections {
		collectSelection(&collected, own, selection)
	}
	// An implicit object of another package is one no syntax names, so nothing tells how it is used.
	for _, implicit := range info.Implicits {
		collected.add(own, implicit, UseWhole)
	}
	switch {
	case info.Types == nil || len(files) == 0:
		collected.closureOnly = "its syntax or expression types were not there to tell its uses apart"
	case linknamed(files):
		collected.closureOnly = "it names a symbol by go:linkname, past the type checker"
	default:
		walkUses(&collected, own, info, files)
	}
	return collected
}

// collectSelection adds what selection reads of other packages: the selection itself, from the first named type of
// another package on its way, and each embedded field of another package it passes through on its way to what it
// selects, which the package never names, so no use holds it: `outer.Value`, where another package's Outer embeds an
// inner struct declaring Value, reads Outer's embedded inner. Each such field is read as a selection of its own name
// from that first type too, since a rule reaches it so: time_until's fix names the embedded time.Time by its name
// where that name selects it, which a shallower field of the name in another build would change. The selection's way
// through types of the package's own is the package's own source, the same in every build its own inputs are.
//
// Mutates: collected gains the selection and each such field
func collectSelection(collected *typeReads, own *types.Package, selection *types.Selection) {
	path := selection.Index()
	current := selection.Recv()
	var receiver *types.Named
	start := 0
	for position, index := range path {
		if receiver == nil {
			if receiver = foreignNamed(current, own); receiver != nil {
				start = position
				collected.addSelection(own, receiver, selection.Obj(), path[start:])
			}
		}
		if position == len(path)-1 {
			return
		}
		if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStruct := current.Underlying().(*types.Struct)
		if !isStruct {
			return
		}
		embedded := structure.Field(index)
		collected.add(own, embedded, UseMember)
		if receiver != nil {
			collected.addSelection(own, receiver, embedded, path[start:position+1])
		}
		current = embedded.Type()
	}
}

// addSelection records the selection of selected, by its name, from receiver, another package's named type, at index.
// A member of the package's own reached through another package's type still selects what the package's own inputs
// pin, so it is kept as it is.
func (collected *typeReads) addSelection(
	own *types.Package,
	receiver *types.Named,
	selected types.Object,
	index []int,
) {
	declared, isForeign := foreignDeclared(selected, own)
	if !isForeign {
		declared = selected
	}
	read := Selection{Receiver: receiver.Origin().Obj(), Name: selected.Name(), Index: slices.Clone(index),
		Selected: declared}
	collected.selections[selectionKey(read)] = read
}

// foreignNamed is the named type of another package than own that current is or points to, nil for any other type.
func foreignNamed(current types.Type, own *types.Package) *types.Named {
	pointedTo := types.Unalias(current)
	if pointer, isPointer := pointedTo.(*types.Pointer); isPointer {
		pointedTo = types.Unalias(pointer.Elem())
	}
	named, isNamed := pointedTo.(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || named.Obj().Pkg() == own {
		return nil
	}
	return named
}

// linknamed reports whether any of files holds a go:linkname directive.
func linknamed(files []*ast.File) bool {
	for _, file := range files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:linkname ") {
					return true
				}
			}
		}
	}
	return false
}

// foreignDeclared is object as its package declares it, the generic one for a field, method or function of an
// instance, and whether that package is another than own. An object of no package, the universe's, is no other's.
func foreignDeclared(object types.Object, own *types.Package) (types.Object, bool) {
	if object == nil || object.Pkg() == nil || object.Pkg() == own {
		return nil, false
	}
	switch instantiated := object.(type) {
	case *types.Func:
		return instantiated.Origin(), true
	case *types.Var:
		return instantiated.Origin(), true
	}
	return object, true
}
