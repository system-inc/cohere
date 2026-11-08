package parity

import "fmt"

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type numberOrText interface {
	~int | ~string
}

type exact interface {
	int | uint
}

type both interface {
	integer
	numberOrText
}

type empty interface {
	int
	string
}

type named int

func (named) String() string { return "" }

// Type parameters match a verb when every type of their type set does.
func typeParameters[I integer, N numberOrText, E exact, B both, M empty, A any, S fmt.Stringer, R error,
	P *int, Q interface{ ~[]int }, T interface{ *named }, U interface{ named | int }](
	i I, n N, e E, b B, m M, a A, s S, r R, p P, q Q, t T, u U) {
	fmt.Printf("%d %x", i, i) // Clean.
	fmt.Printf("%s", i)       // Wrong type, contains ~int.
	fmt.Printf("%d", n)       // Wrong type, contains ~string.
	fmt.Printf("%s", e)       // Wrong type, contains int.
	fmt.Printf("%d %s", b, b) // Clean, and wrong type, contains ~int: the intersection is ~int.
	fmt.Printf("%d", m)       // Clean: an empty type set is not computed.
	fmt.Printf("%d", a)       // Wrong type.
	fmt.Printf("%v %T", a, a) // Clean.
	fmt.Printf("%s", s)       // Clean, a Stringer.
	fmt.Printf("%d", s)       // Wrong type.
	fmt.Printf("%s", r)       // Clean, an error.
	_ = fmt.Errorf("%w", r)   // Clean.
	_ = fmt.Errorf("%w", s)   // Wrong type.
	fmt.Printf("%d", p)       // Wrong type, a pointer constraint.
	fmt.Printf("%d %s", q, q) // Clean, and wrong type.
	fmt.Printf("%s", t)       // Clean, *named has String.
	fmt.Printf("%s %d", u, u) // Wrong type, contains int, and clean.
	fmt.Printf("%*d", i, 1)   // Clean, an integer width.
	fmt.Printf("%*d", n, 1)   // Non-int as argument of *, contains ~string.
	fmt.Printf("%d", &i)      // Clean: a pointer to a type parameter gives up.
	fmt.Printf("%d", []I{})   // Clean.
	fmt.Printf("%s", []N{})   // Wrong type.
}

// A generic print wrapper's own parameters.
func typeParameterWrapper[T any](value T, arguments ...any) {
	fmt.Print(arguments...)
}

func callsTypeParameterWrapper() {
	typeParameterWrapper(1, "%d", 2) // Possible directive.
}
