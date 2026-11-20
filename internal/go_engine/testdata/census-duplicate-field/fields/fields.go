// Package fields declares a struct whose field a is named twice, a type error the type checker reports while it still
// records the struct with one field a, so its syntax holds more fields than its type.
package fields

// Shape names its field a twice.
type Shape struct {
	a int
	a string
}

// Size reads the shape's first field.
func Size(shape Shape) int { return shape.a }
