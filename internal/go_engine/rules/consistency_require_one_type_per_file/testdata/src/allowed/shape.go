package allowed

// An interface lists methods but declares none, and is never a receiver, so it never counts beside a type with
// methods.
type Shape interface {
	Area() float64
}

type Circle struct {
	radius float64
}

func (circle Circle) Area() float64 {
	return 3 * circle.radius * circle.radius
}
