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

// A letter another package declared is used here, not declared, and is not ours to rename.
func corner(rectangle image.Rectangle) image.Point {
	return image.Point{X: rectangle.Min.X, Y: rectangle.Max.Y}
}
