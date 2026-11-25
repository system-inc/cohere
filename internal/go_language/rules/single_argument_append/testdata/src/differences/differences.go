// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: the port matches what the pattern matches. The places a rewrite of the pattern
// could part from it are the parentheses its matcher looks through, the ellipsis it has no field for, and the
// arguments it counts in syntax rather than values, so these calls hold each, and a port that judged any of them
// otherwise would show here as a finding one side makes and the other does not.
package differences

func use(values ...any) {}

func triple() ([]string, string, string) { return nil, "", "" }

// Found by both: parentheses on the function, its argument and the call at once.
func parentheses(values []int) {
	use((append)((values)), (append(values)))
}

// Found by both: one argument in syntax, three values, two of them appended.
func threeValues() {
	use(append(triple()))
}

// Found by neither: the spread has an argument before it.
func spread(values []int) {
	use(append(values, values...))
}
