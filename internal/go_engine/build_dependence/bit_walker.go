package build_dependence

import (
	"go/types"
)

// reference is one bit of an object a unit or a declaration reads: its name bit, the only bit of anything but a named
// type, or with touchesUnderlying set a named type's underlying bit.
type reference struct {
	object            types.Object
	touchesUnderlying bool
}

// walkKey is one type walked in one mode, read in full or as a name.
type walkKey struct {
	walked types.Type
	full   bool
}

// bitWalker collects the bits a type reaches. Read in full, as a value whose layout or operations matter, a named type
// gives both its bits; read as a name, behind a pointer, slice, map, chan, signature or interface method, only its
// name bit, since none of those reads what lies under it. An array element or struct field keeps the mode it was
// reached in, since a value holds them inline. An interface's embedded types and a union's terms are read in full,
// since they are its method set and its type set.
//
// A type of the universe is the same in every build and gives nothing, and a type declared in a function is walked
// through to what lies under it, since no unit declares it.
type bitWalker struct {
	seen      map[walkKey]bool
	collected []reference
}

// references are the bits walked reaches, read in full or as a name, each once.
func (walker *bitWalker) references(walked types.Type, full bool) []reference {
	clear(walker.seen)
	walker.collected = walker.collected[:0]
	walker.walk(walked, full)
	return append([]reference(nil), walker.collected...)
}

// emit records object's name bit, and with full its underlying bit too.
func (walker *bitWalker) emit(object types.Object, full bool) {
	walker.collected = append(walker.collected, reference{object: object})
	if full {
		walker.collected = append(walker.collected, reference{object: object, touchesUnderlying: true})
	}
}

// walk collects what walked reaches in its mode.
func (walker *bitWalker) walk(walked types.Type, full bool) {
	if walked == nil {
		return
	}
	key := walkKey{walked: walked, full: full}
	if walker.seen[key] {
		return
	}
	walker.seen[key] = true
	switch walked := walked.(type) {
	case *types.Named:
		object := walked.Origin().Obj()
		switch {
		case universal(object):
		case local(object):
			walker.walk(walked.Underlying(), full)
		default:
			walker.emit(object, full)
		}
		for argument := range walked.TypeArgs().Types() {
			walker.walk(argument, full)
		}
	case *types.Alias:
		object := walked.Origin().Obj()
		switch {
		case universal(object):
		case local(object):
			walker.walk(walked.Rhs(), full)
		default:
			walker.emit(object, full)
		}
		for argument := range walked.TypeArgs().Types() {
			walker.walk(argument, full)
		}
	case *types.TypeParam:
		walker.walk(walked.Constraint(), full)
	case *types.Pointer:
		walker.walk(walked.Elem(), false)
	case *types.Slice:
		walker.walk(walked.Elem(), false)
	case *types.Chan:
		walker.walk(walked.Elem(), false)
	case *types.Map:
		walker.walk(walked.Key(), false)
		walker.walk(walked.Elem(), false)
	case *types.Array:
		walker.walk(walked.Elem(), full)
	case *types.Struct:
		for field := range walked.Fields() {
			walker.walk(field.Type(), full)
		}
	case *types.Signature:
		for parameter := range walked.TypeParams().TypeParams() {
			walker.walk(parameter.Constraint(), false)
		}
		walker.walk(walked.Params(), false)
		walker.walk(walked.Results(), false)
	case *types.Tuple:
		for variable := range walked.Variables() {
			walker.walk(variable.Type(), full)
		}
	case *types.Interface:
		for method := range walked.ExplicitMethods() {
			walker.walk(method.Type(), false)
		}
		for embedded := range walked.EmbeddedTypes() {
			walker.walk(embedded, true)
		}
	case *types.Union:
		for term := range walked.Terms() {
			walker.walk(term.Type(), true)
		}
	}
}
