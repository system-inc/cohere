// Package lower is a package facts imports, whose functions the host's analysis of facts reads answers about
// (#5vbs1np).
package lower

// Depth is a function another package calls.
func Depth() int { return 3 }

// Halve takes a slice as its second parameter, the parameter a read is of.
func Halve(count int, values []string) []string { return values[:count/2] }

// Scale is a type with a method that takes a parameter.
type Scale struct{}

// Apply is a method whose parameter a read is of.
func (Scale) Apply(value int) int { return value * 2 }

// Limit is a value, which no read about a function names.
var Limit = 4

// Hook is a value of a function type, whose parameter is no function's.
var Hook func(value int) int
