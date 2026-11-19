// Package collide declares one and two at one place, as cmd/cgo places its generated functions.
package collide

//line shared.go:1
func two() int {
	return 2
}

// alone is declared at a place of its own, which a test places as any other.
func alone(count int) int {
	return count
}
