package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// TestTheIgnoreNoteReadsTheRepositorysRules: whether a project's cache would be tracked is asked of git's ignore rules
// as the repository sees them, from its root down to the project. A project nested in a repository whose root
// `.gitignore` ignores `.cache/` is not told its cache would be tracked, as every project of a multi-project root
// once was; one in a repository whose rules do not ignore it still is, when its cache directory was there already
// without the ignore file a run that creates it writes (#6vhp0vd). One whose run creates the directory is not told,
// since that cache ignores itself.
func TestTheIgnoreNoteReadsTheRepositorysRules(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, fixture := range []struct {
		rootRules string
		// existing is whether the project's cache directory is there before the run, as an older cohere made it.
		existing bool
		wantTold bool
	}{
		{rootRules: ".cache/\n", existing: true, wantTold: false},
		{rootRules: "node_modules/\n", existing: true, wantTold: true},
		{rootRules: "node_modules/\n", existing: false, wantTold: false},
	} {
		root := newRepository(test, map[string]string{
			".gitignore":                   fixture.rootRules,
			"apps/web/tsconfig.json":       incrementalFixtureConfiguration + "\n",
			"apps/web/CohereSettings.json": "{ \"rules\": {} }\n",
			"apps/web/index.ts":            "export const value = 1;\n",
		})
		project := filepath.Join(root, "apps", "web")
		if fixture.existing {
			if createDirectoryError := os.MkdirAll(cache_directory.Of(project), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
		}
		command := exec.Command(binary, "--no-fix", "--no-format", "--verbose")
		command.Dir = project
		command.Env = append(os.Environ(), "COHERE_VERDICT_FD=")
		var stderr bytes.Buffer
		command.Stdout, command.Stderr = &bytes.Buffer{}, &stderr
		requireChildRan(test, command.Run())
		if told := strings.Contains(stderr.String(), "does not ignore .cache/"); told != fixture.wantTold {
			test.Errorf("under a root ignoring %q, the cache there already %t, the project was told its cache "+
				"would be tracked: %t, want %t:\n%s",
				strings.TrimSpace(fixture.rootRules), fixture.existing, told, fixture.wantTold, stderr.String())
		}
	}
}
