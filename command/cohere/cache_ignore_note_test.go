package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheIgnoreNoteReadsTheRepositorysRules: whether a project's cache would be tracked is asked of git's ignore rules
// as the repository sees them, from its root down to the project. A project nested in a repository whose root
// `.gitignore` ignores `.cache/` is not told its cache would be tracked, as every project of a multi-project root
// once was; one in a repository whose rules do not ignore it still is.
func TestTheIgnoreNoteReadsTheRepositorysRules(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, rootRules := range []string{".cache/\n", "node_modules/\n"} {
		root := newRepository(test, map[string]string{
			".gitignore":                   rootRules,
			"apps/web/tsconfig.json":       incrementalFixtureConfiguration + "\n",
			"apps/web/CohereSettings.json": "{ \"rules\": {} }\n",
			"apps/web/index.ts":            "export const value = 1;\n",
		})
		command := exec.Command(binary, "--no-fix", "--no-format", "--verbose")
		command.Dir = filepath.Join(root, "apps", "web")
		command.Env = append(os.Environ(), "COHERE_VERDICT_FD=")
		var stderr bytes.Buffer
		command.Stdout, command.Stderr = &bytes.Buffer{}, &stderr
		requireChildRan(test, command.Run())
		told := strings.Contains(stderr.String(), "does not ignore .cache/")
		if wantTold := rootRules != ".cache/\n"; told != wantTold {
			test.Errorf("under a root ignoring %q the project was told its cache would be tracked: %t, want %t:\n%s",
				strings.TrimSpace(rootRules), told, wantTold, stderr.String())
		}
	}
}
