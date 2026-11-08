package parity

import (
	"fmt"

	"parity/other"
	. "parity/other"
)

type Shape interface {
	Area() float64
}

type OtherShape interface {
	Area() float64
}

// ShapeAlias is the very type Shape names.
type ShapeAlias = Shape

type Square struct{}

func (Square) Area() float64 { return 1 }

type Container[Element any] interface {
	Get() Element
}

func assertions(shape Shape, alias ShapeAlias, anything any, empty interface{}, failure error, stringer fmt.Stringer) {
	// The asserted type the value already has, in each shape the value and the type can be written, each reported.
	// The message renders an alias by the alias's name.
	_ = shape.(Shape)
	_ = (shape).(Shape)
	_ = shape.((Shape))
	_ = shape.(ShapeAlias)
	_ = alias.(Shape)
	_ = alias.(ShapeAlias)
	_ = anything.(any)
	_ = anything.(interface{})
	_ = empty.(any)
	_ = failure.(error)
	_ = stringer.(fmt.Stringer)
	_, _ = shape.(Shape)

	// A value of an interface type that is not the asserted one, though its methods are the same: none reported, since
	// a named interface and a literal of its methods are not identical.
	_ = shape.(OtherShape)
	_ = shape.(interface{ Area() float64 })
	_ = anything.(Shape)
	_ = shape.(fmt.Stringer)

	// A concrete asserted type is never an interface: none reported.
	_ = shape.(Square)
	_ = anything.(int)

	// A type switch has no asserted type: none reported.
	switch shape.(type) {
	case Shape:
	}
	switch typed := anything.(type) {
	case any:
		_ = typed
	}
}

func literals(value interface{ Area() float64 }, spread interface {
	Area() float64
	Perimeter() float64
}) {
	// An interface literal asserted to an identical literal, written on one line and on several, reported with the
	// literal rendered over its lines.
	_ = value.(interface{ Area() float64 })
	_ = spread.(interface {
		Area() float64
		Perimeter() float64
	})
	// The same methods in another order are the same interface: reported, rendered in the asserted order.
	_ = spread.(interface {
		Perimeter() float64
		Area() float64
	})
	// Comments inside the asserted type: reported, and the message carries both, since each method holds its own.
	_ = spread.(interface {
		// The area comes first.
		Area() float64 // and this trails it.
		Perimeter() float64
	})
	// The named interface of the same methods is another type: none reported.
	_ = value.(Shape)
}

func expressions(shapes []Shape, lookup map[string]Shape, function func() Shape, holder struct{ shape Shape }) {
	// The value written as an index, a map lookup, a call, a field and a conversion, each reported.
	_ = shapes[0].(Shape)
	_ = lookup["square"].(Shape)
	_ = function().(Shape)
	_ = holder.shape.(Shape)
	_ = Shape(Square{}).(Shape)
	_ = (Shape)(Square{}).(Shape)
	// An assertion of an assertion, each of the same type, is two findings.
	_ = shape().(Shape).(Shape)
	// A value spread over several lines: reported, and rendered over them.
	_ = pick(
		"square",
		1,
	).(Shape)
}

func shape() Shape { return Square{} }

func pick(string, int) Shape { return Square{} }

func otherPackage(reader other.Reader, dotted Reader) {
	// Another package's interface, named through its package and through the dot import, is the one type: each
	// reported, the type rendered as written.
	_ = reader.(other.Reader)
	_ = reader.(Reader)
	_ = dotted.(other.Reader)
}

func generics[Parameter Shape, Free any](
	value Container[int],
	boxed other.Box[string],
	parameterized Container[Parameter],
	anything any,
	shape Shape,
) {
	// A generic interface instantiated alike is the same type, reported, and instantiated apart another, not.
	_ = value.(Container[int])
	_ = value.(Container[string])
	_ = boxed.(other.Box[string])
	_ = boxed.(Box[string])
	_ = parameterized.(Container[Parameter])
	_ = parameterized.(Container[Free])
	// A type parameter's underlying type is its constraint, an interface, and it is identical to no value's type:
	// none reported.
	_ = anything.(Free)
	_ = shape.(Parameter)
}
