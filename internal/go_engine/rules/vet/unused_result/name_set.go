package unused_result

import (
	"maps"
	"slices"
	"strings"
)

// nameSet is the value of one of the pass's two flags, a set of names written as a comma-separated list. x/tools'
// stringSetFlag, whose text it reads and prints the same way, so a setting and its default read as before the port.
type nameSet map[string]bool

// String is the set's names, sorted and joined by commas, which the flag package also records as the default.
func (names *nameSet) String() string {
	return strings.Join(slices.Sorted(maps.Keys(*names)), ",")
}

// Set replaces the set with the names text lists, as the original does: a setting never adds to the default. An
// empty entry, as in `a,,b` or a trailing comma, is skipped, and empty text is the empty set.
func (names *nameSet) Set(text string) error {
	replaced := nameSet{}
	if text != "" {
		for name := range strings.SplitSeq(text, ",") {
			if name == "" {
				continue
			}
			replaced[name] = true
		}
	}
	*names = replaced
	return nil
}
