package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestModulePathsReadsEachModuleTheRootGoverns: a go.work's uses, each by the path its go.mod names, in the go.work's
// order; a module on its own by its go.mod; and nothing for a root with neither, or a use whose go.mod is missing,
// which describing the project refuses in its own words.
func TestModulePathsReadsEachModuleTheRootGoverns(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "a workspace",
			files: map[string]string{
				"go.work":          "go 1.27\n\nuse (\n\t./ours\n\t./outside\n\t./missing\n)\n",
				"ours/go.mod":      "module github.com/system-inc/ours\n\ngo 1.27\n",
				"outside/go.mod":   "module example.com/outside\n\ngo 1.27\n",
				"missing/notes.md": "no go.mod here\n",
			},
			want: []string{"github.com/system-inc/ours", "example.com/outside"},
		},
		{
			name:  "a module",
			files: map[string]string{"go.mod": "module example.com/alone\n\ngo 1.27\n"},
			want:  []string{"example.com/alone"},
		},
		{name: "neither", files: map[string]string{"notes.md": "nothing\n"}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			for name, contents := range testCase.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
					test.Fatal(makeError)
				}
				if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			if got := ModulePaths(root); !slices.Equal(got, testCase.want) {
				test.Errorf("ModulePaths read %q, want %q", got, testCase.want)
			}
		})
	}
}
