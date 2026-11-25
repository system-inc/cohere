package workspace

import (
	"sort"
	"strings"
)

// PlatformLoad is the tree as another platform builds it. Clean holds the packages that type-check there, each
// with everything it imports; Failed names the rest, with the first error, since a package that does not
// type-check under a platform cannot say what its files name there.
type PlatformLoad struct {
	Platform Platform
	Clean    *Workspace
	Failed   map[string]string
}

// FailedSummary lists the failed packages, sorted, each with its error, for a reader.
func (load *PlatformLoad) FailedSummary() []string {
	lines := make([]string, 0, len(load.Failed))
	for id, reason := range load.Failed {
		lines = append(lines, id+": "+strings.ReplaceAll(reason, "\n", " "))
	}
	sort.Strings(lines)
	return lines
}
