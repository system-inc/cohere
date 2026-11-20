// Package observed holds members whose own type spells an unnamed struct (#mtfdb1m). Such a member's own line writes
// each unexported field of the struct that is not embedded by its properties in the build, its size first, and the
// struct has no name to hang an underlying bit on, so the member's bit reads what each such field's properties read.
// Pad's size and Guard's word move on linux while every declaration here reads alike.
package observed

// Pad is Length bytes, so its underlying moves on linux and its name does not.
type Pad struct {
	_ [Length]byte
}

// Guard holds a word, so how it prints moves on linux, as its underlying does.
type Guard struct {
	w word
}

// Shelf holds a slice of Guards, so it reads Guard by name alone, and neither of its bits is set.
type Shelf struct {
	guards []Guard
}

// Held holds a Pad by value in an unexported field of an unnamed struct, 4 bytes on the host and 8 on linux: set.
var Held struct {
	_    Pad
	Flag bool
}

// Behind points at an unnamed struct that holds a Pad by value, whose field its own line writes all the same: set.
var Behind *struct{ _ Pad }

// Shelved holds a Shelf by value, which prints as the Guards behind its slice do: set, through Shelf's declaration.
var Shelved struct {
	_ Shelf
}

// Pointed holds a Pad through a pointer, as long and printed alike whatever it points at: unset.
var Pointed struct {
	_ *Pad
}

// Guarded holds a Guard through a pointer, the same: unset.
var Guarded struct {
	_ *Guard
}

// Named is a Pad itself, written by its name: unset.
var Named Pad

// Exported holds a Pad in an exported field, which its line enters and writes by name: unset.
var Exported struct {
	Pad Pad
}

// Embedded holds a Pad in an embedded field, the same: unset.
var Embedded struct {
	Pad
}

// Give gives an unnamed struct that holds a Pad by value, a signature's: set.
func Give() (given struct{ _ Pad }) {
	return given
}

// Outer is a named struct whose field Inner is an unnamed struct that holds a Pad by value: Inner is set.
type Outer struct {
	Inner struct {
		_ Pad
	}
}

// Taker is an interface whose method Take takes an unnamed struct that holds a Pad by value: Take is set.
type Taker interface {
	Take(struct{ _ Pad })
}

// Inferred declares no type, so no unit declares the fields of the struct its initializer spells, and Inner is read
// through its type wherever it is read: both its bits set.
var Inferred = struct {
	Inner struct {
		_ Pad
	}
}{}

// SelectsLocal selects Inner from a local, whose unnamed struct no unit declares, so the read of Inner is of what its
// type reaches, the Guards behind its slice among them: dependent.
func SelectsLocal() any {
	var local struct {
		Inner struct{ _ []Guard }
	}
	return local.Inner
}

// PassesLocal hands on a local whose unnamed struct holds a slice of Guards, which no unit declares: the local is read
// through its type as a package-level var of it is, its observed field's Guards among it: dependent.
func PassesLocal() any {
	var local struct {
		_ []Guard
	}
	return local
}

// Box holds a slice of what it is told to hold.
type Box[Held any] struct {
	items []Held
}

// Sharer's method Share takes a struct holding one instance twice, Box of a pointer to an unnamed struct that holds a
// Pad: in an observed field, whose line never enters the instance's type argument, and in an exported one, whose line
// does and writes the Pad field by its size. The method's bit is one walk of its parameters, the fields being the
// interface's, and that walk meets the type argument both ways, so what it read in the observation must not stand for
// what the member's walk reads: Share is set.
type Sharer interface {
	Share(struct {
		_    Box[*struct{ _ Pad }]
		Open Box[*struct{ _ Pad }]
	})
}

// ReentersLocal hands on a local that holds, in an observed field, a Box of itself, and beside it a Shelf by value.
// Observing the field meets the local again inside the local's own walk, behind Box's slice, and an observation of it
// reads the Shelf and the Guards behind its slice, as a package-level var of the same struct with a named type in the
// local's place observes them: dependent. The observed field that meets it is Box's own, declared at package level,
// so every read of the body meets it inside the local's walk; `back struct{ _ []local }` would not do, its field `_`
// being declared in the body, whose read of the field's declaration observes the local afresh.
func ReentersLocal() any {
	type local struct {
		back  Box[local]
		Shelf Shelf
	}
	var held local
	return held
}
