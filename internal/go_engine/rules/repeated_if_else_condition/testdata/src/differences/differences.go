// Package differences holds the chains where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: conditions that differ only in parentheses or a line break, which staticcheck's
// Render prints alike or apart for both, a repeat after an else if whose initializer stops the chain, and repeats
// inside the closures of a chain's conditions.
package differences

func plausible(first bool, second bool, count int) {
	if !(first) {
	} else if !first {
	}
	if (first) && second {
	} else if first && second {
	}
	if first &&
		second {
	} else if first && second {
	}
	if first {
	} else if size := count; size > 0 {
	} else if first {
	} else if first {
	}
	if func() bool { return first }() {
	} else if func() bool { return first }() {
	}
	if first {
	} else if second {
	} else if first || second {
	} else if second || first {
	}
}
