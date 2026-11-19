// Package placed declares what a test of PlacedIn places answers about: a function objectpath names and one it does
// not, a method, whose receiver is no parameter, and a function after a //line directive, which names another file.
package placed

// Exported is exported, so objectpath names it, and its answer is placed as any other is.
func Exported(values []int) int {
	return len(values)
}

// hidden is unexported, so objectpath names no path to it, and only its place names an answer about it.
func hidden() int {
	return 1
}

// Holder has a method, whose receiver is not counted among its parameters.
type Holder struct{}

// Method takes two parameters, second at index 1.
func (holder Holder) Method(first int, second []int) {}

// directed is placed in the file the directive below names, at its offset in this one.
//
//line written.go:40
func directed() int {
	return 2
}
