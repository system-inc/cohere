//go:build cgo

// Package native adds through C where the build runs cgo, and in Go where it doesn't.
package native

/*
static int add(int left, int right) { return left + right; }
typedef struct { int width; int height; } box;
*/
import "C"

// Kind says which way the build adds.
const Kind = "cgo"

// Number is the C int the build adds in.
type Number = C.int

// Box is a box's sides, as C lays them out.
type Box C.box

// Add adds left and right in C.
func Add(left Number, right Number) Number {
	return C.add(left, right)
}

// Area is box's area.
func Area(box Box) int {
	return int(box.width * box.height)
}
