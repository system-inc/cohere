package printf

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

// stringSet is a set of names, the value of the -funcs flag: x/tools' printf stringSet. A name without a dot is
// held lowercased, since it matches a function's name in any case.
type stringSet map[string]bool

// String is the set's names, sorted and joined by commas, the flag's value as the engine's keys read it.
func (set stringSet) String() string {
	return strings.Join(slices.Sorted(maps.Keys(set)), ",")
}

// Set adds each name of the comma-separated list, or is the error that one is empty.
func (set stringSet) Set(list string) error {
	for name := range strings.SplitSeq(list, ",") {
		if name == "" {
			return errors.New("empty string")
		}
		if strings.Contains(name, ".") {
			set[name] = true
		} else {
			set[strings.ToLower(name)] = true
		}
	}
	return nil
}
