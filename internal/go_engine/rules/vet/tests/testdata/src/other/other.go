// Package other is imported by parity's tests, so an example there can name what only an import declares, and an
// identifier two packages declare is looked up in both.
package other

// Buffer shares its name with parity's Buffer and has a method parity's lacks.
type Buffer struct{}

// Reset is a method only other's Buffer has.
func (Buffer) Reset() {}

// T shares its name with testing's T, so a test taking *other.T looks like one taking *testing.T.
type T struct{}

// Helper is declared only here.
func Helper() {}
