package layout

import "example.com/layout/dependency"

// Length reads Size, whose value differs by build: dependent.
func Length() int { return Size }

// Doubled is computed from Size: dependent.
const Doubled = Size * 2

// Table's length is Size: dependent.
var Table [Size]int

// Name calls a function each platform's file declares, an object of a seed unit: dependent.
func Name() string { return platformName() }

// Describe reads Mode, which only the tag build changes.
func Describe() string { return Mode }

// Holder has an unexported field whose array length hangs on Size, so its underlying type differs by build and its
// name does not.
type Holder struct {
	slots [Size]int
	Count int
}

// CountOf selects a field of Holder, whose offset hangs on Size: dependent.
func CountOf(holder *Holder) int { return holder.Count }

// Forward hands a Holder on by pointer only, which never reads its layout: independent.
func Forward(holder *Holder) *Holder { return holder }

// PassesHolder gets a Holder's pointer from a constructor in another file, never names Holder, and only passes the
// pointer on and calls a method on it, so it records exactly alike in every build: independent.
func PassesHolder() int {
	holder := Forward(NewHolder())
	return holder.Capacity()
}

// Shell embeds a pointer to Holder, which promotes Holder's fields.
type Shell struct {
	*Holder
}

// ShellCount selects Count through the embedded Holder, whose offset hangs on Size: dependent.
func ShellCount(shell *Shell) int { return shell.Count }

// Inner's underlying hangs on Size.
type Inner struct {
	slots [Size]int
}

// Opaque holds an Inner by value, so its underlying hangs on Size too, while it prints Inner by name.
type Opaque struct {
	inner Inner
}

// Keep uses Opaque only by name, through a pointer: independent.
func Keep(opaque *Opaque) *Opaque { return opaque }

// Box is a generic shared type.
type Box[T any] struct {
	value T
}

// Boxes instantiates Box with Opaque, whose constraint satisfaction reads Opaque's underlying: dependent.
func Boxes() int {
	var box Box[Opaque]
	_ = box
	return 0
}

// ReadsSize's body reads Size: dependent.
func ReadsSize() int { return Size + 1 }

// CallsReadsSize merely calls a function whose body is dependent, and bodies do not propagate: independent.
func CallsReadsSize() int { return ReadsSize() }

// Untouched reads nothing that differs: independent.
func Untouched() int { return 1 }

// CellOf builds a dependency's Cell, whose underlying hangs on the dependency's Width: dependent.
func CellOf() dependency.Cell { return dependency.Cell{} }

// Cells reads the dependency's Width: dependent.
func Cells() int { return dependency.Width }

// Labelled uses the dependency's Label, which no build changes: independent.
func Labelled() string { return dependency.Label }

// CallsWide calls the dependency's Wide, whose body reads Width, and bodies do not propagate: independent, and a
// static caller of Wide, which the facts census follows (#ss4t5g6).
func CallsWide() bool { return dependency.Wide() }
