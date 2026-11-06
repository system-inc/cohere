// Copyright notice and other comments may sit above the constraint.

//go:build !purego

package allowed

// Any build tag names a variant, the toolchain's own words and the house's alike.
type Journal struct {
	lines []string
}

func (journal *Journal) Write(line string) {
	journal.lines = append(journal.lines, line)
}
