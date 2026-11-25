// Package architecturesizes is the architecture differential's fixture (#mtfdb1m): Pad is as long as padSize, which
// each architecture's file sets, 64 on amd64 and 128 on arm64, as internal/cpu's CacheLinePad is as long as its
// CacheLinePadSize, and Guard holds a word, which each architecture's file declares, an int32 on amd64 and a func() on
// arm64. Every file but those two is the same in both builds, so nothing below differs by what it spells, only by
// what lies under Pad and Guard, and each declaration holds one of them another way, so a failure names which ways an
// object's own line moves with what its bit does not read.
package architecturesizes

import "example.com/architecturesizes/dependency"

// Pad is padSize bytes, so its underlying moves with the architecture while its name, a struct spelled in place,
// reads alike.
type Pad struct {
	_ [padSize]byte
}

// Features holds two Pads by value in unexported fields of an unnamed struct, as internal/cpu's X86 and ARM64 hold
// CacheLinePad around their flags: each Pad field is 64 bytes on amd64 and 128 on arm64.
var Features struct {
	_   Pad
	Has bool
	_   Pad
}

// Pointed holds a Pad through a pointer, 8 bytes on both architectures.
var Pointed struct {
	_ *Pad
}

// Arrayed holds two Pads by value in an unnamed array, 128 bytes on amd64 and 256 on arm64.
var Arrayed struct {
	_ [2]Pad
}

// Nested holds a Pad by value one unnamed struct deeper, under an exported field.
var Nested struct {
	Inner struct {
		_ Pad
	}
}

// Foreign holds the dependency's Pad by value, a named type of another package.
var Foreign struct {
	_ dependency.Pad
}

// Named is a Pad itself, a variable of the named type.
var Named Pad

// Exported holds a Pad by value in an exported field, which a reader can name.
var Exported struct {
	Pad Pad
}

// Embedded holds a Pad by value in an embedded field, whose fields it promotes.
var Embedded struct {
	Pad
}

// Map holds two Pads by value, and Table a Map, as runtime's traceStackTable holds a traceMap that holds
// CacheLinePad: neither spells anything that moves, and each one's underlying moves with Pad's.
type Map struct {
	_    Pad
	next *Map
	_    Pad
}

// Table holds a Map by value.
type Table struct {
	tab Map
}

// Tabled holds two Tables by value, as runtime's trace holds its stackTab: Pad is three named types down.
var Tabled struct {
	_ [2]Table
}

// Give gives an unnamed struct that holds a Pad by value, a signature rather than a variable.
func Give() (given struct{ _ Pad }) {
	return given
}

// Outer is a named struct whose exported field is an unnamed struct that holds a Pad by value.
type Outer struct {
	Inner struct {
		_ Pad
	}
}

// Behind points at an unnamed struct that holds a Pad by value: the struct is behind a pointer, and Pad by value in it.
var Behind *struct{ _ Pad }

// Sliced is a slice of such a struct.
var Sliced []struct{ _ Pad }

// Mapped is a map to such a struct.
var Mapped map[string]struct{ _ Pad }

// Channeled is a channel of such a struct.
var Channeled chan struct{ _ Pad }

// Called is a function that takes such a struct.
var Called func(struct{ _ Pad })

// Deeper holds a Pad by value one unnamed struct deeper, under an unexported field.
var Deeper struct {
	inner struct{ _ Pad }
}

// Wrapped is a named struct whose unexported field points at an unnamed struct that holds a Pad by value: its own
// size is a pointer's in both builds.
type Wrapped struct {
	_ *struct{ _ Pad }
}

// Taker is an interface whose method takes an unnamed struct that holds a Pad by value.
type Taker interface {
	Take(struct{ _ Pad })
}

// Guard holds a word, so what prints and compares of it moves with the architecture, as its underlying does.
type Guard struct {
	w word
}

// Guarded holds a Guard through a pointer, which prints and compares as a pointer whatever it points at, so its own
// line reads alike in both builds.
var Guarded struct {
	_ *Guard
}

// Listed holds a slice of Guards, which prints as its elements do, with the verbs a word takes.
var Listed struct {
	_ []Guard
}

// Keyed holds a map to Guards, which prints as its keys and elements do.
var Keyed struct {
	_ map[string]Guard
}

// Shelf holds a slice of Guards, so it reads Guard by name alone, behind the slice.
type Shelf struct {
	guards []Guard
}

// Shelved holds a Shelf by value, which prints as the Guards its slice holds do, one named type down.
var Shelved struct {
	_ Shelf
}

// Stacked holds a slice of Shelves, a slice down and then a named type's own slice down.
var Stacked struct {
	_ []Shelf
}

// Box holds a slice of what it is told to hold, so an instance of it prints as that prints.
type Box[Held any] struct {
	items []Held
}

// Boxed holds a Box of Guards by value: the Guards are behind the instance's slice, where its type argument stands.
var Boxed struct {
	_ Box[Guard]
}

// Ref points at what it is told to and holds none of it, as sync/atomic's Pointer does.
type Ref[Target any] struct {
	_  [0]*Target
	at uintptr
}

// Referred holds a Ref to a Guard by value, as long and printed alike whatever a Guard holds, so its own line reads
// alike in both builds.
var Referred struct {
	_ Ref[Guard]
}

// Signalled holds a channel of Guards, which is as long and prints alike whatever it carries, so its own line reads
// alike in both builds.
var Signalled struct {
	_ chan Guard
}

// Handled holds a function that takes a Guard, the same.
var Handled struct {
	_ func(Guard)
}

// Tree holds a slice of itself beside a word, so a walk that enters a named type's declaration through its slices
// meets Tree again inside Tree.
type Tree struct {
	branches []Tree
	w        word
}

// Rooted holds a Tree by value, which prints as its word does.
var Rooted struct {
	_ Tree
}

// Marked holds a mark, which compares in both builds, so a Marked can key a map, which a Guard cannot.
type Marked struct {
	m mark
}

// KeyedBy holds a map keyed by Marked, which prints as its keys do, with the verbs a mark takes.
var KeyedBy struct {
	_ map[Marked]string
}

// Bare is an alias of an unnamed struct that holds a slice of Guards.
type Bare = struct {
	guards []Guard
}

// Aliased holds a Bare by value, which prints as the Guards behind the alias's slice do.
var Aliased struct {
	_ Bare
}

// Spoken holds an interface whose method takes a Guard, which is as long and prints alike whatever the method takes,
// so its own line reads alike in both builds.
var Spoken struct {
	_ interface{ Say(Guard) }
}

// Pointing is an alias of a pointer to an unnamed struct that holds a Pad, so a member spelling Pointing holds no
// field of that struct in its own declaration.
type Pointing = *struct{ _ Pad }

// Linked holds a Pointing: its line writes the field by its properties, those of a pointer, and never enters it, so
// the inner struct's Pad is no line's and its own line reads alike.
var Linked struct {
	_ Pointing
}

// Hook is an alias of a function that takes such a struct.
type Hook = func(struct{ _ Pad })

// Hooked holds a Hook, the same.
var Hooked struct {
	_ Hook
}

// padding is as long as a Pad, an unexported type spelled in place, whose name reads alike in both builds.
type padding struct {
	_ [padSize]byte
}

// Hidden embeds a padding, an unexported field that is embedded, which its line enters and writes by name, so its own
// line reads alike in both builds.
var Hidden struct {
	padding
}

// Constrained is constrained by an unnamed struct that holds a Shelf, a term its line walks in the one mode it walks
// the signature in, writing the field by its properties: it prints as the Guards behind Shelf's slice do.
func Constrained[Value ~struct{ _ Shelf }](Value) {}

// Sized is constrained by a slice of an unnamed struct that holds a Pad by value.
type Sized[Value ~[]struct{ _ Pad }] struct{}

// Size is Sized's method: its line walks its receiver's type parameters' constraints and writes the Pad field by its
// size.
func (Sized[Value]) Size() int {
	return 0
}

// Ring holds a slice of itself with its type arguments swapped, so a read of Ring[Guard, int] meets Ring[int, Guard]
// and then Ring[Guard, int] again, and ends only by knowing an instance it has met.
type Ring[First any, Second any] struct {
	next []Ring[Second, First]
	held First
}

// Ringed holds a Ring of a Guard by value, which prints as its Guard does.
var Ringed struct {
	_ Ring[Guard, int]
}

// Swapped holds a Ring of an int, whose slice holds Rings of a Guard, which print as their Guards do: the instance it
// holds a Guard by value in is the one inside its own.
var Swapped struct {
	_ Ring[int, Guard]
}
