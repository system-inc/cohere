// Package shadowed declares its own float64 and float32 at package scope, an integer type and an alias of float64,
// which the pattern's Object accepts by name alone, so both sides report each conversion, naming the type the code
// declares.
package shadowed

type float64 int

type float32 = float64

func conversions() {
	_ = -float64(0)
	_ = float64(-0)
	_ = -float32(0)
	_ = float32(-0)
	_ = -0.0
}
