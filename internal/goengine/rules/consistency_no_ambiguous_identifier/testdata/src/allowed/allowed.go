package allowed

import (
	"image"
	"strings"
)

// Words are what the rule asks for.
const limit = 3

func readFirst(reader *strings.Reader) (byte, error) {
	first, readError := reader.ReadByte()
	return first, readError
}

// The coordinates keep their letters, as variables, parameters, fields and results.
type Coordinate struct {
	x, y, z float64
}

func lengthSquared(x, y, z float64) float64 {
	return x*x + y*y + z*z
}

func doubled(point Coordinate) (x float64, y float64) {
	x = point.x * 2
	y = point.y * 2
	return x, y
}

func scaled(point Coordinate) Coordinate {
	x := point.x * 2
	var y = point.y * 2
	z := point.z * 2
	return Coordinate{x: x, y: y, z: z}
}

// The blank identifier is Go's grammar for no name, wherever it stands.
var _ = limit

type _ struct{}

func (_ Coordinate) zero() {}

func ignore(_ int, _ string) (_ bool) {
	return false
}

func lengths(words []string) int {
	total := 0
	for _, word := range words {
		_ = word
		total++
	}
	return total
}

// An uppercase letter is a type parameter by convention, and both twins leave it alone, nexus's
// consistency_no_ambiguous_identifier.go and Swift's ConsistencyNoAmbiguousIdentifier.swift, which says it follows
// this line, so the three engines judge the same names: type parameters, receivers' type parameters, constants,
// types, functions and exported coordinate fields all keep an uppercase letter.
const N = 3

type Point struct {
	X, Y, Z int
}

type List[T any] struct {
	items []T
}

func (list *List[T]) Push(item T) {
	list.items = append(list.items, item)
}

func Map[T, U any](items []T, convert func(T) U) []U {
	results := make([]U, 0, len(items))
	for _, item := range items {
		results = append(results, convert(item))
	}
	return results
}

type E struct{}

func F() {}

// A letter another package declared is used here, not declared, and is not ours to rename.
func corner(rectangle image.Rectangle) image.Point {
	return image.Point{X: rectangle.Min.X, Y: rectangle.Max.Y}
}
