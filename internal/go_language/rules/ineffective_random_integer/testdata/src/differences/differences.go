// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original walks the calls staticcheck's type index records for each function it
// names and the port walks every call, so a call the index reaches through parentheses, a selector and an
// instantiation at once is here, and a call written over several lines, whose message holds the call as
// report.Render writes it, where a reach or a rendering of the port's own would show as a finding one side makes and
// the other does not.
package differences

import "math/rand/v2"

func use(values ...any) {}

// Found by both: parentheses, a selector and an instantiation at once.
func everyLayer() {
	use((rand.N[int64])((+1)))
}

// Found by both: a call over several lines, rendered on one.
func severalLines(generator *rand.Rand) {
	use(generator.IntN(
		1,
	))
}

// Found by neither: a bound of 2.
func notNamed() {
	use(rand.Int64N(2))
}
