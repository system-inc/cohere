// Package library is the package parity imports its struct types from, since only an imported struct's unkeyed
// literal is judged.
package library

import "image"

// Struct is a struct of exported fields alone, so its unkeyed literal is offered field names.
type Struct struct{ F int }

// Other has Struct's underlying type, so a constraint may hold both and a literal of either.
type Other struct{ F int }

// Pair has a field each of two types.
type Pair struct {
	Name  string
	Count int
}

// Embedding embeds a struct and a pointer to one, each field named for its type.
type Embedding struct {
	Struct
	*Pair
	Count int
}

// Generic is a generic struct, instantiated by its literal.
type Generic[Value any] struct {
	Value Value
	Count int
}

// Empty has no fields.
type Empty struct{}

// Alias is another package's name for Struct.
type Alias = Struct

// PointAlias is another name for an allow-listed type.
type PointAlias = image.Point

// Rows is a named slice of an anonymous struct, whose elided element is a struct of no package.
type Rows []struct{ A, B int }

// Structs is a named slice of Struct.
type Structs []Struct

// Pointers is a named slice of pointers to Struct.
type Pointers []*Struct

// Lookup is a named map of Struct to Struct.
type Lookup map[Struct]Struct

// StructPointer is a named pointer to Struct.
type StructPointer *Struct

// Constraint is an interface of Struct's type set alone.
type Constraint interface{ Struct }

// TildeConstraint is the set of types whose underlying type is Struct's.
type TildeConstraint interface{ ~struct{ F int } }
