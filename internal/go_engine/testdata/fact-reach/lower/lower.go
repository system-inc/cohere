// Package lower is a package facts imports, about whose functions the host holds answers of its own (#5vbs1np).
package lower

// Depth is a function another package calls.
func Depth() int { return 3 }

// Halve takes a slice as its second parameter, the parameter an answer is of, and is called by another package.
func Halve(count int, values []string) []string { return values[:count/2] }

// Scale is a type with a method no other package calls.
type Scale struct{}

// Apply is a method no other package uses, so an answer about it or its parameter is no question of theirs.
func (Scale) Apply(value int) int { return value * 2 }

// Limit is a value, which no answer about a function names.
var Limit = 4
