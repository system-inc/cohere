package return_nilness

import (
	"slices"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// state is the nilness of the values at one point of a function: each value's ValueNilness by its number, a value
// past the end holding the lattice's identity, and whether values is this state's own copy yet, since a state starts
// sharing the slice the analysis handed it and copies it only on its first change.
type state struct {
	cloned    bool
	values    []ValueNilness
	numbering numbering
}

// get is value's nilness: {_ NeverNil} for a value that cannot be nil, what the state holds, or, for a value it holds
// nothing of, the default of its kind.
func (current *state) get(value intermediate_representation.Value) ValueNilness {
	if !typeutil.MaybePointerLike(value.Type()) {
		// Every type that is not pointer like is always {_ NeverNil}.
		return ValueNilness{Outer: NeverNil}
	}
	number := current.numbering.number(value)
	if number < len(current.values) {
		return current.values[number]
	}

	switch value.(type) {
	case *intermediate_representation.Parameter:
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	case *intermediate_representation.Builtin:
		return ValueNilness{Outer: NeverNil}
	case *intermediate_representation.FreeVariable:
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	case *intermediate_representation.Function:
		return ValueNilness{Outer: NeverNil}
	case *intermediate_representation.Global:
		// A global is an address, not the value stored at it, and an address cannot be nil.
		return ValueNilness{Outer: NeverNil}
	}

	return lattice{}.Ident()
}

// set records key's nilness as value.
func (current *state) set(key intermediate_representation.Value, value ValueNilness) {
	if !typeutil.MaybePointerLike(key.Type()) {
		// A type that is not pointer like is always {_ NeverNil}, so there is nothing to record.
		return
	}

	if value == (lattice{}.Ident()) {
		// The default value is not stored.
		return
	}
	number := current.numbering.number(key)
	if !current.cloned {
		if number < len(current.values) && current.values[number] == value {
			// No copy is made when the value already matches.
			return
		}
		current.cloned = true
		current.values = slices.Clone(current.values)
	}
	if number >= len(current.values) {
		current.values = append(current.values, make([]ValueNilness, number-len(current.values)+1)...)
	}
	current.values[number] = value
}

// setInner records the nilness of the value key holds as value, keeping what is known of key itself.
func (current *state) setInner(key intermediate_representation.Value, value Nilness) {
	if !typeutil.MaybePointerLike(key.Type()) {
		return
	}
	if value == (lattice{}.Ident().Inner) {
		return
	}
	number := current.numbering.number(key)
	if !current.cloned {
		if number < len(current.values) && current.values[number].Inner == value {
			// No copy is made when the value already matches.
			return
		}
		current.cloned = true
		current.values = slices.Clone(current.values)
	}
	if number >= len(current.values) {
		fallback := current.get(key)
		current.values = append(current.values, make([]ValueNilness, number-len(current.values)+1)...)
		current.values[number] = fallback
	}
	updated := current.values[number]
	updated.Inner = value
	current.values[number] = updated
}

// setOuter records the nilness of key itself as value, keeping what is known of the value it holds.
func (current *state) setOuter(key intermediate_representation.Value, value Nilness) {
	if !typeutil.MaybePointerLike(key.Type()) {
		return
	}
	if value == (lattice{}).Ident().Outer {
		return
	}
	number := current.numbering.number(key)
	if !current.cloned {
		if number < len(current.values) && current.values[number].Outer == value {
			// No copy is made when the value already matches.
			return
		}
		current.cloned = true
		current.values = slices.Clone(current.values)
	}
	if number >= len(current.values) {
		fallback := current.get(key)
		current.values = append(current.values, make([]ValueNilness, number-len(current.values)+1)...)
		current.values[number] = fallback
	}
	updated := current.values[number]
	updated.Outer = value
	current.values[number] = updated
}
