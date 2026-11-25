package load

import (
	"path/filepath"
	"strings"
	"testing"
)

// inside answers as workspace with a separator appended, as a prefix, answers, over every pairing of workspaces and
// paths at its edges: the workspace itself, a sibling whose name runs on past the workspace's, the root, the empty
// workspace, a trailing separator and a path shorter than the workspace.
func TestInsideIsTheWorkspaceAndASeparatorAsAPrefix(test *testing.T) {
	test.Parallel()
	separator := string(filepath.Separator)
	workspaces := []string{"", separator, separator + "a", separator + "a" + separator + "b",
		separator + "a" + separator + "b" + separator}
	var paths []string
	for _, workspace := range workspaces {
		paths = append(paths, workspace, workspace+separator, workspace+"c", workspace+separator+"c",
			workspace+separator+separator+"c", workspace+"c"+separator+"d")
	}
	paths = append(paths, "", "a", separator+"ab", separator+"a"+separator+"bc")
	for _, workspace := range workspaces {
		for _, path := range paths {
			want := strings.HasPrefix(path, workspace+separator)
			if got := inside(workspace, path); got != want {
				test.Errorf("inside(%q, %q) is %t, want %t", workspace, path, got, want)
			}
		}
	}
}
