package configuration

// IgnoreTally counts the paths each of a list of ignore globs matches, compiled once and asked of every path a
// walk considers (#cxeq6jw). A walk asks it before any ignore applies, git's or the project's, so an entry
// whose paths exist always counts as live, whatever else also keeps them out of the run.
type IgnoreTally struct {
	patterns []string
	globs    []glob

	// whole is whether each glob takes everything below a directory it matches. See TakesWholeDirectory.
	whole []bool

	// Matched is how many paths each glob has matched, by the glob's index.
	Matched []int
}

// NewIgnoreTally compiles patterns, globs relative to one directory, for counting.
func NewIgnoreTally(patterns []string) *IgnoreTally {
	tally := &IgnoreTally{
		patterns: patterns,
		globs:    make([]glob, len(patterns)),
		whole:    make([]bool, len(patterns)),
		Matched:  make([]int, len(patterns)),
	}
	for index, pattern := range patterns {
		tally.globs[index] = compileGlob(pattern)
		tally.whole[index] = TakesWholeDirectory(pattern)
	}
	return tally
}

// Fresh is a tally over the same compiled globs with counts of its own, for a walk on another goroutine to fill
// and Add back.
func (tally *IgnoreTally) Fresh() *IgnoreTally {
	return &IgnoreTally{
		patterns: tally.patterns,
		globs:    tally.globs,
		whole:    tally.whole,
		Matched:  make([]int, len(tally.globs)),
	}
}

// Unmatched is the index of each glob that has matched nothing yet.
func (tally *IgnoreTally) Unmatched() []int {
	var unmatched []int
	for index, count := range tally.Matched {
		if count == 0 {
			unmatched = append(unmatched, index)
		}
	}
	return unmatched
}

// LiteralPrefix is the glob at index's literal leading segments. See LiteralPrefix.
func (tally *IgnoreTally) LiteralPrefix(index int) string {
	return LiteralPrefix(tally.patterns[index])
}

// CountOne is Count for the glob at index alone, and reports whether that glob takes the path whole, a directory
// a walk counting for it need not enter.
func (tally *IgnoreTally) CountOne(index int, relativePath string, directory bool) bool {
	if directory && !tally.whole[index] {
		return false
	}
	if !tally.globs[index].matches(splitPath(relativePath)) {
		return false
	}
	tally.Matched[index]++
	return directory
}

// Add adds another tally's counts to this one's, the two over the same globs.
func (tally *IgnoreTally) Add(other *IgnoreTally) {
	for index, count := range other.Matched {
		tally.Matched[index] += count
	}
}

// TakesWhole reports whether any glob takes a directory whole, so a walk counting for liveness has no need to
// enter it: the glob is live by the directory alone.
func (tally *IgnoreTally) TakesWhole(relativeDirectory string) bool {
	segments := splitPath(relativeDirectory)
	for index, pattern := range tally.globs {
		if tally.whole[index] && pattern.matches(segments) {
			return true
		}
	}
	return false
}

// Count adds one for each glob that matches a path, slash-separated and relative to the globs' directory. A
// directory is matched only by a glob that takes it whole, as the format walk prunes one, so `*.code.js` never
// counts a directory that happens to share the name.
func (tally *IgnoreTally) Count(relativePath string, directory bool) {
	if len(tally.globs) == 0 {
		return
	}
	segments := splitPath(relativePath)
	for index, pattern := range tally.globs {
		if directory && !tally.whole[index] {
			continue
		}
		if pattern.matches(segments) {
			tally.Matched[index]++
		}
	}
}
