package return_nilness

// Nilness is what is known of whether a value is nil. Its zero value is no knowledge at all, the lattice's identity.
type Nilness uint8

const (
	// NeverNil is a value that is never nil.
	NeverNil Nilness = iota + 1
	// AlwaysNil is a value that is always nil.
	AlwaysNil
	// MaybeNilGlobal is a value that might be nil, but only because of the value of a global variable.
	MaybeNilGlobal
	// MaybeNil is a value that might be nil.
	MaybeNil
)

// String names nilness as staticcheck's Nilness does, so the facts print alike.
func (nilness Nilness) String() string {
	switch nilness {
	case 0:
		return "NoNilness"
	case NeverNil:
		return "NeverNil"
	case AlwaysNil:
		return "AlwaysNil"
	case MaybeNilGlobal:
		return "MaybeNilGlobal"
	case MaybeNil:
		return "MaybeNil"
	default:
		return "InvalidNilness"
	}
}

// ValueNilness is the nilness of one value, its fields in staticcheck's order, so it prints as staticcheck's does.
type ValueNilness struct {
	// Inner is undefined for a value that is no interface. For an interface value, it is whether the value stored in
	// it may be nil. Even when Outer is MaybeNil, Inner may still say something for when the interface value is not
	// nil: {NeverNil MaybeNil} is an interface value that might be nil but, when it is not, holds a value that is not.
	Inner Nilness
	// Outer is, for a value that is no interface, whether it may be nil, and for an interface value, whether the
	// interface value itself may be nil.
	Outer Nilness
}
