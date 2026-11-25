// Package cycle holds two functions that call each other, so the nilness of their results is the push's only in the
// order the push walks the package in, and a question of one is answered by replaying that walk.
package cycle

// Shape is what Outer and Inner return.
type Shape interface {
	Area() int
}

// Square is a Shape.
type Square struct{}

// Area is a square's area.
func (*Square) Area() int {
	return 1
}

// Outer returns a fresh square, and first asks Inner for one depth less.
func Outer(depth int) Shape {
	if depth > 0 {
		_ = Inner(depth - 1)
	}
	return &Square{}
}

// Inner returns what Outer returns.
func Inner(depth int) Shape {
	return Outer(depth)
}

// Present is whether Inner returned a shape, which asks the nilness of Inner's result.
func Present() bool {
	return Inner(3) != nil
}
