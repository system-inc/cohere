package build_dependence

import (
	"slices"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates"
)

// reference is one bit of an object a unit or a declaration reads: its name bit, the only bit of anything but a named
// type, or with touchesUnderlying set a named type's underlying bit.
type reference struct {
	object            types.Object
	touchesUnderlying bool
}

// walkKey is one type walked in one mode: read in full, as a name, with member set as a member's own type
// (borrowedMemberReferences), or with observed set as an observed field's type (bitWalker.observe). A type met as a
// name in a member's walk reads more than one met as a name in an observation, so the two are never one walk's seen
// type.
type walkKey struct {
	walked   types.Type
	full     bool
	member   bool
	observed bool
}

// bitWalker collects the bits a type reaches. Read in full, as a value whose layout or operations matter, a named type
// gives both its bits; read as a name, behind a pointer, slice, map, chan, signature or interface method, only its
// name bit, since none of those reads what lies under it. An array element or struct field keeps the mode it was
// reached in, since a value holds them inline. An interface's embedded types and a union's terms are read in full,
// since they are its method set and its type set.
//
// A type of the universe is the same in every build and gives nothing, and a type declared in a function is walked
// through to what lies under it, since no unit declares it.
//
// A member's own type, a var's, a field's or a signature's, is read as a name with one thing more (#mtfdb1m): the
// bit of a member owes whatever the member's own line shows (check's useLines), and that line shows, for every
// unexported field that is not embedded of every unnamed struct its type reaches before a named type, behind
// pointers, slices, arrays, maps, channels and signatures and inside other unnamed structs, the field's properties in
// the build, its size first (type_predicates.Observe). An unnamed struct has no node and no underlying bit of its own
// to carry what lies under its fields, and a selection through one records no receiver, so the member that spells it
// is the one read that carries its layout, and the member's bit reads each such field's type as an observation can
// read it (observe). So one object has one line and one bit, and no reader of a bit has to know a second rule.
// `var X86 struct{ _ CacheLinePad; HasAES bool }` reads CacheLinePad's underlying, 64 bytes on amd64 and 128 on arm64,
// which its name alone never tells.
//
// The line walks a signature's type parameters' constraints in that one mode, a method's receiver's among them, so a
// struct a constraint spells as a term or an embedded element, which the walk meets in full, has its observed fields
// observed too, and a method's bit reads its receiver's constraints as a member's own type (collectFunction). The line
// never enters an observed field, so what an observation reads of one is read with the member's walk off: an unnamed
// struct behind a pointer or a signature inside it is a name, as long and printing alike whatever its fields hold.
type bitWalker struct {
	// seen are the types the current walk has walked.
	seen      map[walkKey]bool
	collected []reference

	// entering is set for a walk that reads in full behind pointers, slices, maps, channels, signatures and interface
	// methods too (borrowedEnteringReferences).
	entering bool

	// member is set for a walk of a member's own type, which observes the observed fields of each unnamed struct it
	// meets while reading as a name (borrowedMemberReferences).
	member bool

	// within are the origins of the instances the walk is entering, through to what lies under each, each with
	// whether an observation entered it (enter).
	within []entered
}

// entered is an origin the walk is within, and whether an observation entered it (observeInstance) or a walk did,
// which enters only a local (walk).
type entered struct {
	origin   types.Object
	observed bool
}

// borrowedReferences are the bits walked reaches, read in full or as a name, each once, in the walker's own buffer,
// which its next walk writes over: for a caller that reads them at once and keeps none, as addEdges and
// namer.through do, so no copy is made for each walk.
func (walker *bitWalker) borrowedReferences(walked types.Type, full bool) []reference {
	clear(walker.seen)
	walker.collected = walker.collected[:0]
	walker.walk(walked, full)
	return walker.collected
}

// borrowedEnteringReferences are borrowedReferences' bits of walked read in full, everything it reaches entered in
// full too, behind pointers, slices, maps, channels, signatures and interface methods as well: whatever a type taken
// from walked by an operation could hold, as an element, a pointer's target or a result, for a var whose type its
// initializer infers (collector.collectValue).
func (walker *bitWalker) borrowedEnteringReferences(walked types.Type) []reference {
	walker.entering = true
	referenced := walker.borrowedReferences(walked, true)
	walker.entering = false
	return referenced
}

// borrowedUnderlyingReferences are the bits a named type's underlying bit reads: underlying read in full, as a value,
// and with it what a body reads through the type with no expression of its own to stop at (#mtfdb1m). A selection
// through a defined pointer type dereferences it, `counted.counter` for `type Counted *struct{ pad Pad; counter int64
// }`, and so does a field promoted through an embedded pointer, `holder.counter` for `type Holder struct{ *Inner }`.
// The body's expressions are a Counted and a *Holder, and nothing in it is the struct behind the pointer, so its reads
// stop at the name, and the bit the selection reads of its receiver, the receiver's underlying, is the one read that
// can carry the struct's layout, a field's offset among it. So the element behind every pointer at the top of
// underlying is read in full, `type P **T` as well, since the read costs nothing and needs no argument about how many
// levels a rule dereferences, and so is the element of each embedded pointer field of the struct found there. A
// pointer or a struct spelled through an alias is the type it stands for (types.Unalias). One embedded further down is
// a named type's own or an alias's, since an embedded field is spelled by a type's name, and that type's underlying
// bit reads it in turn: `Inner` embedding `*Deep` reads Deep and Holder reads Inner, and
// `type Shared = struct{ *Inner }` reads Inner for a struct that embeds Shared, an alias's underlying bit reading its
// target as this does (collectTypeKinds). In full means as a struct's fields are read in full, exported ones too, never
// as an observation reads them (observe): the selection reads the struct's layout whole.
//
// Neither read turns on a word size, and no other read of the walk does, so no part of the bits assumes a 64-bit host:
// SA1027 judges a field's offset on a 32-bit build alone, and the bits read alike there. Two tests hold it: the
// pointed fixture's, loaded as 386 against arm as well as amd64 against arm64
// (TestANamedTypesUnderlyingReadsAStructBehindAPointerNoExpressionNames), and go_engine's body-reach-pointed fixture's
// (TestABodyReachingAStructThroughAPointerNoExpressionNamesIsReached).
func (walker *bitWalker) borrowedUnderlyingReferences(underlying types.Type) []reference {
	clear(walker.seen)
	walker.collected = walker.collected[:0]
	walker.walk(underlying, true)
	// What a selection dereferences: the element behind the last pointer at the top, underlying itself where none is.
	dereferenced := underlying
	for {
		pointer, isPointer := types.Unalias(dereferenced).(*types.Pointer)
		if !isPointer {
			break
		}
		dereferenced = pointer.Elem()
		walker.walk(dereferenced, true)
	}
	if structure, isStruct := types.Unalias(dereferenced).(*types.Struct); isStruct {
		for field := range structure.Fields() {
			if pointer, isPointer := types.Unalias(field.Type()).(*types.Pointer); isPointer && field.Embedded() {
				walker.walk(pointer.Elem(), true)
			}
		}
	}
	return walker.collected
}

// borrowedMemberReferences are borrowedReferences' bits of walked read as a name, walked being a member's own type:
// a var's or a field's type, a signature, or its parameters or results. Each observed field of each unnamed struct
// the walk meets as a name is observed (observe), as the member's own line writes it.
func (walker *bitWalker) borrowedMemberReferences(walked types.Type) []reference {
	walker.member = true
	referenced := walker.borrowedReferences(walked, false)
	walker.member = false
	return referenced
}

// throughKey is how every reader of an object no unit declares reads it through its type (namer.through,
// Dependence.ObjectBits, IdentifiedObjects.Bits, Counterparts.objectByName, bodyWalk.addObject's local): a constant's
// type in full, since its value is under it, and any other object's as a member's own type (borrowedMemberReferences),
// since a field of an unnamed struct in an initializer or a body, which no unit declares, has a line of its own that
// observes as a declared field's does, and a local of an unnamed struct type is read as a package-level var of it is.
func throughKey(object types.Object) walkKey {
	if _, isConstant := object.(*types.Const); isConstant {
		return walkKey{walked: object.Type(), full: true}
	}
	return walkKey{walked: object.Type(), member: true}
}

// borrowedKeyReferences are the bits key's type reaches in key's mode, read in full, as a name, or as a member's own
// type, in the walker's own buffer.
func (walker *bitWalker) borrowedKeyReferences(key walkKey) []reference {
	if key.member {
		return walker.borrowedMemberReferences(key.walked)
	}
	return walker.borrowedReferences(key.walked, key.full)
}

// borrowedThroughReferences are the bits reading object reaches through its type (throughKey), in the walker's own
// buffer: none of its readers asks the walker again before it is done with them, each reading the bits at once.
func (walker *bitWalker) borrowedThroughReferences(object types.Object) []reference {
	return walker.borrowedKeyReferences(throughKey(object))
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
	key := walkKey{walked: walked, full: full, member: walker.member}
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
			if walker.enter(object, false) {
				walker.walk(walked.Underlying(), full)
				walker.leave()
			}
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
			if walker.enter(object, false) {
				walker.walk(walked.Rhs(), full)
				walker.leave()
			}
		default:
			walker.emit(object, full)
		}
		for argument := range walked.TypeArgs().Types() {
			walker.walk(argument, full)
		}
	case *types.TypeParam:
		walker.walk(walked.Constraint(), full)
	case *types.Pointer:
		walker.walk(walked.Elem(), walker.entering)
	case *types.Slice:
		walker.walk(walked.Elem(), walker.entering)
	case *types.Chan:
		walker.walk(walked.Elem(), walker.entering)
	case *types.Map:
		walker.walk(walked.Key(), walker.entering)
		walker.walk(walked.Elem(), walker.entering)
	case *types.Array:
		walker.walk(walked.Elem(), full)
	case *types.Struct:
		for field := range walked.Fields() {
			// The renderer's own test of a field it writes by its properties rather than enters (check's
			// closureWalkStep), so the two cannot name different fields. The renderer walks a constraint's terms and
			// embedded elements in its one mode, so a struct the member's walk meets in full there is observed as one
			// met as a name is, and still read in full. Both reads are with the member's walk off (bitWalker), the line
			// never entering the field.
			if walker.member && type_predicates.ObservedOnly(field) {
				walker.member = false
				walker.observe(field.Type())
				if full {
					walker.walk(field.Type(), true)
				}
				walker.member = true
				continue
			}
			walker.walk(field.Type(), full)
		}
	case *types.Signature:
		for parameter := range walked.TypeParams().TypeParams() {
			walker.walk(parameter.Constraint(), walker.entering)
		}
		walker.walk(walked.Params(), walker.entering)
		walker.walk(walked.Results(), walker.entering)
	case *types.Tuple:
		for variable := range walked.Variables() {
			walker.walk(variable.Type(), full)
		}
	case *types.Interface:
		for method := range walked.ExplicitMethods() {
			walker.walk(method.Type(), walker.entering)
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

// observe collects what an observation of observed, an observed field's type, can read (type_predicates.Observe):
// more than a read in full, which stops at each named type and reads only a name behind a slice or a map. Each named
// type on the way gives both its bits, an alias its name bit alone (observeInstance), and is entered in turn, its
// declaration walked as observed is: its underlying bit carries what it holds by value and never what its own slices
// and maps hold, which it reads by name, while the field's line moves with those, `struct{ _ Shelf }` printing as the
// elements of Shelf's slice do. A type met again is left alone (seen), so one that holds a slice of itself ends the
// walk, and an instance met within one of its own origin is read by its type arguments, observed, never entered again
// (enter), so one that holds itself at a larger argument ends it too. An instance's type arguments are otherwise read
// by name: what the instance holds of them is in its underlying, entered with them in place, and `atomic.Pointer[g]`
// holds no g.
//
// It mirrors the reach of each observation of type_predicates/observe.go's observations, no further:
//   - size and alignment (the build's Sizeof and Alignof) and comparable (types.Comparable) read what a type holds by
//     value, a struct's fields and an array's elements, each named type's underlying entered;
//   - the lock (locks.Of, type_predicates/locks/lock_path.go) reads the same, and each type's Lock and Unlock, which
//     its name bit carries;
//   - printf (verbs.MatchNested, the match of type_predicates/verbs/argument_matcher.go) reads the same and, in its
//     map, array and slice cases, a map's keys and elements and a slice's elements, which is why those are entered
//     here; of a pointer below the argument's own it reads only which kind its target is (matchPointer), and of each
//     type its String, Error and Format, which its name bit carries;
//   - binary (BinaryEncodable), float (MayHoldFloat) and basic (OnlyBasicValues) read a struct's fields and an
//     array's elements.
//
// So a pointer, a channel, a signature and an interface are read in full as ever, a name behind each: no observation
// looks behind one, a pointer being as long and printing alike whatever it points at. An observation added to
// Observe that reaches further, behind a pointer say, must widen this walk with it. What holds the two alike is the
// object differential: over the architecture_sizes fixture of check's testdata, each shape of which has a line that
// moves by one observation, and over the standard library by architecture
// (TestEveryStandardLibraryObjectWhoseLinesDifferHasItsBitSet), each failing where a line moves and this walk did not
// reach what moved it; and the fixture's members whose lines read alike, pinned unset, fail where it reaches further
// than any observation does.
func (walker *bitWalker) observe(observed types.Type) {
	if observed == nil {
		return
	}
	key := walkKey{walked: observed, full: true, observed: true}
	if walker.seen[key] {
		return
	}
	walker.seen[key] = true
	switch observed := observed.(type) {
	case *types.Named:
		walker.observeInstance(observed.Origin().Obj(), observed.TypeArgs(), observed.Underlying(), true)
	case *types.Alias:
		walker.observeInstance(observed.Origin().Obj(), observed.TypeArgs(), observed.Rhs(), false)
	case *types.Array:
		walker.observe(observed.Elem())
	case *types.Slice:
		walker.observe(observed.Elem())
	case *types.Map:
		walker.observe(observed.Key())
		walker.observe(observed.Elem())
	case *types.Struct:
		for field := range observed.Fields() {
			walker.observe(field.Type())
		}
	default:
		walker.walk(observed, true)
	}
}

// observeInstance observes a named type or an alias, object being its origin's, arguments its type arguments and under
// what it stands for: its name bit, with underlyingBit its underlying bit too, its arguments by name, and under,
// entered (enter). One the walk is already entering an instance of has its arguments observed instead.
//
// A named type gives both bits, its underlying bit being the one that reads what its declaration's expression uses, an
// array's length among it. An alias gives its name bit alone, which reads those uses too (collectTypeKinds): its
// underlying bit adds its target read in full, which observing under reads already, and what a selection through the
// target dereferences behind a pointer (borrowedUnderlyingReferences), which no observation reads. So a field of
// `type Pointing = *struct{ _ Pad }` reads Pad by name, as a pointer's properties do.
func (walker *bitWalker) observeInstance(
	object types.Object,
	arguments *types.TypeList,
	under types.Type,
	underlyingBit bool,
) {
	if universal(object) {
		return
	}
	if !local(object) {
		walker.emit(object, underlyingBit)
	}
	if !walker.enter(object, true) {
		for argument := range arguments.Types() {
			walker.observe(argument)
		}
		return
	}
	// Each argument's name, which under already holds for any instance go/types expanded, its arguments substituted
	// in: read here too for a load whose expansion failed, where under may not hold them. No fixture can tell the two
	// apart for that reason, and none pins this loop.
	for argument := range arguments.Types() {
		walker.walk(argument, false)
	}
	walker.observe(under)
	walker.leave()
}

// enter adds origin to the origins the walk is within, entered by an observation where observed is set, and reports
// true, or reports false where the walk is already within an instance of it entered the same way, whose own walk then
// reads it (#mtfdb1m). An instance names its type arguments, so the seen set, keyed by the instance, ends a walk only
// where the instances it meets repeat: go/types hands back one instance for identical arguments, so Tree inside Tree
// and a Ring that swaps its two arguments end there, but a generic that holds itself at a larger argument,
// `type Grown[Held any] struct{ next []Grown[[]Held] }`, meets a new instance at each step. go/types refuses that as
// an instantiation cycle and still types it, and a load hands the walk broken code as readily as any, so the walk ends
// by the origin instead: an instance met within one of its own origin is not entered again. What it holds is its
// origin's declaration, which the instance being entered reads already, with its type arguments in place of the
// parameters, so reading the arguments in its stead reads at least as much: an observation observes them, and a walk
// walks them in its mode, as it does every instance's.
//
// The two ways are kept apart, since only within one way does the declaration already being read read as much. A
// walk enters only a local, as a name, in full or as a member's own type, and an observation reads more than any of
// those, so an observation that meets a local within the local's own walk enters it again: reading its type arguments
// in its stead would read nothing of a local that has none, and `type local struct{ back Box[local]; Shelf Shelf }`
// read so never observes Shelf or the Guards behind its slice. Each entry adds an origin the walk is not within that
// way, of which a program declares finitely many, so a path holds each at most twice, and every other step goes into
// a part of the type it is at, so a walk ends on any load.
func (walker *bitWalker) enter(origin types.Object, observed bool) bool {
	entry := entered{origin: origin, observed: observed}
	if slices.Contains(walker.within, entry) {
		return false
	}
	walker.within = append(walker.within, entry)
	return true
}

// leave takes the last origin enter added off the origins the walk is within.
func (walker *bitWalker) leave() {
	walker.within = walker.within[:len(walker.within)-1]
}
