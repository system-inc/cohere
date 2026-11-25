// The value a `Primitive` instruction holds.
package high_level_intermediate_representation

// PrimitiveValue is the literal a `Primitive` holds, one variant per kind of literal.
//
// A closed sum rather than `any` because the kinds the language tells apart must stay apart here.
// Held as `any`, a number, a string and a bigint were all the same Go string, and `null` and a
// missing value were both Go's nil, so no reader could tell `1` from `"1"` or `null` from
// undefined. Each variant is its own type, and a reader names the kinds it means.
//
// The set is closed the same way `InstructionValue` is: the unexported marker method means only
// this package can add a variant.
type PrimitiveValue interface {
	primitiveValue()
}

// PrimitiveNull is the `null` keyword.
type PrimitiveNull struct{}

// PrimitiveUndefined is undefined: the `undefined` identifier, or a value the source leaves out,
// such as a bare `return;` or the alternate of an optional chain.
type PrimitiveUndefined struct{}

// PrimitiveBoolean is `true` or `false`.
type PrimitiveBoolean bool

// PrimitiveNumber is a numeric literal, held as its source text so `0x10` stays `0x10`.
type PrimitiveNumber string

// PrimitiveString is a string literal or a template without substitutions, held as its cooked
// text, without the quotes. A method call's property name lowers to one as well.
type PrimitiveString string

// PrimitiveBigInt is a bigint literal, held as its source text, the `n` suffix included.
type PrimitiveBigInt string

func (PrimitiveNull) primitiveValue()      {}
func (PrimitiveUndefined) primitiveValue() {}
func (PrimitiveBoolean) primitiveValue()   {}
func (PrimitiveNumber) primitiveValue()    {}
func (PrimitiveString) primitiveValue()    {}
func (PrimitiveBigInt) primitiveValue()    {}
