// Package holder declares a struct whose field another package reads without importing this one.
package holder

const Value = 1

type Box struct {
	Count int
}
