// Package expandinginstance holds a generic type that holds a slice of itself at a larger type argument, which
// go/types refuses as an instantiation cycle and still types: Grown[int] holds []Grown[[]int], which holds
// []Grown[[][]int], each an instance no walk has met. A walk of block 1 must end over it all the same, broken code
// being a load's as much as any other (#mtfdb1m).
package expandinginstance

// Grown holds a slice of itself at a slice of what it holds.
type Grown[Held any] struct {
	next []Grown[[]Held]
	held Held
}

// Growing holds a Grown by value in an unexported field of an unnamed struct, which a member's walk observes.
var Growing struct {
	_ Grown[int]
}

// Grows declares a Grown of its own, which no unit declares, so a walk of it goes through to what lies under it.
func Grows() any {
	type local[Held any] struct {
		next []local[[]Held]
		held Held
	}
	var growing local[int]
	return growing
}
