// Package pointed holds named types whose underlying reaches a struct behind a pointer that no expression names
// (#mtfdb1m). A selection through a defined pointer type dereferences it, and so does a field promoted through an
// embedded pointer, with no node of the struct's type in the body, so the named type's underlying bit is the one read
// that carries the struct's layout. Pad's length moves with the architecture, as each file of it says, while every
// declaration here reads alike, so only Pad's underlying bit is seeded through.
package pointed

// Pad is padLength bytes, so its underlying moves with the architecture and its name does not.
type Pad struct {
	_ [padLength]byte
}

// Counted points at an unnamed struct that holds a Pad by value before its counter: its underlying is set.
type Counted *struct {
	pad     Pad
	counter int64
}

// Exposed is Counted with exported fields, which a selection through it reads all the same: its underlying is set.
type Exposed *struct {
	Pad     Pad
	Counter int64
}

// Inner holds a Pad by value before its counter: its underlying is set by Pad's.
type Inner struct {
	pad     Pad
	counter int64
}

// Pointer points at an Inner, a named struct: its underlying is set.
type Pointer *Inner

// Pointers points at a pointer to an Inner: its underlying is set, through every pointer at its top.
type Pointers **Inner

// Holder embeds a pointer to an Inner, whose fields it promotes: its underlying is set.
type Holder struct {
	*Inner
}

// Chained embeds a pointer to a Holder, which promotes Inner's fields in turn: its underlying is set, through
// Holder's.
type Chained struct {
	*Holder
}

// Linked holds a pointer to an Inner in a field it does not embed, so a selection through it names the pointer by an
// expression of its own: neither bit is set.
type Linked struct {
	next *Inner
}

// Listed is a slice of pointers to Inners, whose elements a body names by an index: neither bit is set.
type Listed []*Inner

// InnerPointer spells a pointer to an Inner through an alias: its underlying is set, as Pointer's is.
type InnerPointer = *Inner

// AliasHolder embeds a pointer to an Inner spelled through an alias, whose fields it promotes all the same: its
// underlying is set, as Holder's is.
type AliasHolder struct {
	InnerPointer
}

// Shared spells through an alias an unnamed struct that embeds a pointer to an Inner: its underlying is set, as a
// named type's would be.
type Shared = struct {
	*Inner
}

// Wrapper embeds Shared, which promotes Inner's fields through the pointer Shared embeds: its underlying is set,
// through Shared's.
type Wrapper struct {
	Shared
}

// SharedPointer points at Shared, so a selection through it dereferences to the struct the alias spells and on
// through its embedded pointer: its underlying is set.
type SharedPointer *Shared
