//go:build !cgo

// Package native adds through C where the build runs cgo, and in Go where it doesn't.
package native

// Kind says which way the build adds.
const Kind = "pure"

// Number is the int the build adds in.
type Number = int32

// Box is a box's sides.
type Box struct {
	width  int32
	height int32
}

// Add adds left and right in Go.
func Add(left Number, right Number) Number {
	return left + right
}

// Area is box's area.
func Area(box Box) int {
	return int(box.width * box.height)
}
