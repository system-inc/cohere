// Package parity holds every shape S1016 branches on, and the near miss of each, for the original and the port to
// judge alike.
package parity

import (
	"example.com/parity/other"
)

type t1 struct {
	a int
	b int
}

type t2 struct {
	a int
	b int
}

// t3 is t1's struct by way of t1.
type t3 t1

// tagged differs from t1 in its tags alone, which a conversion ignores from Go 1.8 on.
type tagged struct {
	a int `json:"a"`
	b int `json:"b"`
}

// mixed differs from t1 in a field's type.
type mixed struct {
	a int
	b string
}

// renamedFields differs from t1 in a field's name.
type renamedFields struct {
	a int
	c int
}

type single struct {
	a int
}

type singleTwin struct {
	a int
}

type inner struct {
	x int
}

// embedding holds an embedded field, whose name is its type's.
type embedding struct {
	inner
	b int
}

type embeddingTwin struct {
	inner
	b int
}

// list is a named type whose underlying type is no struct.
type list []int

// number has methods spelled as t1's fields are, so a selector on it is a method value.
type number int

func (number) a() int { return 0 }

func (number) b() int { return 0 }

// functions holds fields of the methods' types.
type functions struct {
	a func() int
	b func() int
}

type generic[T any] struct {
	a T
	b T
}

type genericTwin[T any] struct {
	a T
	b T
}

// alias stands for t2, and aliasOfAlias for alias.
type alias = t2

type aliasOfAlias = alias

// holder holds a t1, so a selector on it selects a field of a field.
type holder struct {
	value t1
}

func shapes() {
	v1 := t1{1, 2}
	v2 := t2{1, 2}
	var v3 t3

	// Positional and keyed, in either order, from a value of another named struct type.
	_ = t2{v1.a, v1.b}       // reported
	_ = t2{a: v1.a, b: v1.b} // reported
	_ = t2{b: v1.b, a: v1.a} // reported
	_ = t3{v1.a, v1.b}       // reported
	_ = t1{v3.a, v3.b}       // reported
	_ = tagged{v1.a, v1.b}   // reported, tags ignored under go1.18
	_ = single{a: singleTwin{a: 1}.a}
	twin := singleTwin{a: 1}
	_ = single{a: twin.a} // reported
	_ = single{twin.a}    // reported
	_ = embeddingTwin{embedding{}.inner, 0}
	embedded := embedding{}
	_ = embeddingTwin{embedded.inner, embedded.b}           // reported
	_ = embeddingTwin{inner: embedded.inner, b: embedded.b} // reported

	// Parenthesized as a whole, and not taken the address of.
	_ = (t2{v1.a, v1.b}) // reported

	// Across lines, with comments among the elements, for the fix's text.
	_ = t2{
		a: v1.a, // the first
		// the second
		b: v1.b,
	}
	_ = t2{
		v1.a,
		v1.b,
	}

	// Near misses: the same type, two sources, fields out of order or misnamed, fields left out, nothing set.
	_ = t1{v1.a, v1.b}
	_ = t3{v1.a, v2.b}
	_ = t2{v1.b, v1.a}
	_ = t2{a: v1.b, b: v1.a}
	_ = t2{a: v1.a}
	_ = t2{}
	_ = mixed{v1.a, ""}
	_ = renamedFields{v1.a, v1.b}
	_ = t2{1, v1.b}
	_ = t2{a: 1, b: v1.b}
	_ = t2{v1.a, 2}

	// The address of the literal is taken, so a conversion would not do.
	_ = &t2{v1.a, v1.b}

	// Parenthesized operands and values the original's assertions refuse.
	_ = t2{(v1).a, (v1).b}
	_ = t2{a: (v1.a), b: (v1.b)}
	held := holder{}
	_ = t2{held.value.a, held.value.b}

	// Sources of no named struct type: a pointer, an unnamed struct, a method value's receiver.
	pointer := &v1
	_ = t2{pointer.a, pointer.b}
	var anonymous struct {
		a int
		b int
	}
	_ = t2{anonymous.a, anonymous.b}
	var count number
	_ = functions{count.a, count.b}

	// Literals of no named struct type: an unnamed struct, a named slice, an element whose type is left out.
	_ = struct {
		a int
		b int
	}{v1.a, v1.b}
	_ = list{v1.a, v1.b}
	_ = []t2{{v1.a, v1.b}}
	_ = map[string]t2{"key": {v1.a, v1.b}}

	// Aliases, on either side, and an alias of an alias. An alias beside the type it stands for is in differences.
	_ = alias{v1.a, v1.b}        // reported
	_ = aliasOfAlias{v1.a, v1.b} // reported
	var aliased alias
	_ = t1{aliased.a, aliased.b} // reported
	_ = alias{aliased.a, aliased.b}

	// Generic instances.
	instance := generic[int]{1, 2}
	_ = genericTwin[int]{instance.a, instance.b} // reported
	_ = generic[int]{instance.a, instance.b}
	_ = t2{instance.a, instance.b} // reported

	// Another package's types: both of it, or one of each.
	couple := other.Couple{A: 1, B: 2}
	_ = other.Pair{couple.A, couple.B}       // reported
	_ = other.Pair{A: couple.A, B: couple.B} // reported
	type local struct {
		A int
		B int
	}
	_ = local{couple.A, couple.B}
	localValue := local{}
	_ = other.Pair{localValue.A, localValue.B}
	_ = other.Pair{other.Value.A, other.Value.B}
}

// Inside a generic function, the instances of its type parameter.
func parameterized[T any](value generic[T], parameter T) {
	_ = genericTwin[T]{value.a, value.b} // reported
	_ = generic[T]{value.a, value.b}
	_ = genericTwin[T]{parameter, parameter}
}

// A function's own types, declared in its body.
func declaredInside() {
	type first struct{ a, b int }
	type second struct{ a, b int }
	value := first{}
	_ = second{value.a, value.b} // reported
	_ = second{a: value.a, b: value.b}
}

// A package-level variable's initializer and a shadowed name.
var packageValue = t1{1, 2}

var _ = t2{packageValue.a, packageValue.b} // reported

func shadowed() {
	packageValue := t3{}
	_ = t2{packageValue.a, packageValue.b} // reported
	{
		v1 := t2{}
		_ = t1{v1.a, v1.b} // reported
	}
}
