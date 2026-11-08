package go_engine

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// FixingRules are the names of the Go rules whose findings a run without --no-fix fixes: the long-line wrap's and the
// rename's, read from the very names those passes act on, so the docs say what a bare run fixes from the code that
// fixes it (#ygqjn5q). Formatting's finding is no registered rule's, so it is not among them.
func FixingRules() []string {
	return slices.Concat([]string{consistency_no_long_line.Name}, renamingRules)
}
