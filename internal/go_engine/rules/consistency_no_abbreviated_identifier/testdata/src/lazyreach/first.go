// Package lazyreach declares no abbreviated name in its first file, so the walk enters it without asking what the
// package reaches, and its first abbreviated name is a type switch's in the second.
package lazyreach

func count(values []any) int {
	return len(values)
}
