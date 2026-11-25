// Package parity holds every shape the pattern accepts or refuses: the operators an assignment can take, the counts
// of its sides, parentheses around the selector and its operand, the types a selector named N can have an operand
// of, and the fields of testing.B and of other types named N.
package parity

import "testing"

type benchmark = testing.B

type pointer = *testing.B

type wrapper struct {
	*testing.B
}

type counter struct {
	N int
}

type named testing.B

func assignments(b *testing.B, other int) {
	b.N = 1
	(b.N) = 2
	(b).N = 3
	(b).N = 4
	b.N += 1
	b.N -= 1
	b.N *= 2
	b.N++
	b.N--
	b.N, other = 1, 2
	other, b.N = 1, 2
	b.N, other = other, b.N
	_ = b.N
	other = b.N
	_ = other
}

func definitions(b *testing.B) {
	n := b.N
	_ = n
}

func types(b *testing.B, value testing.B, alias *benchmark, aliased pointer, embedded wrapper, plain counter,
	plainPointer *counter, renamed *named, twice **testing.B, t *testing.T, f *testing.F) {
	value.N = 1
	alias.N = 1
	aliased.N = 1
	embedded.N = 1
	embedded.B.N = 1
	plain.N = 1
	plainPointer.N = 1
	renamed.N = 1
	(*twice).N = 1
	_ = t
	_ = f
	_ = b
}

func benchmarkFunction(b *testing.B) {
	for range b.N {
		b.N = 0
	}
	func() {
		b.N = 1
	}()
	defer func() { b.N = 1 }()
}

func generic[T any](b *testing.B, value T) T {
	b.N = 1
	return value
}

func index(benchmarks []*testing.B, table map[string]*testing.B) {
	benchmarks[0].N = 1
	table["key"].N = 1
	benchmarks[0].StartTimer()
}
