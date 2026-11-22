package guard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// performanceFile is the file nothing that builds or tests may read.
const performanceFile = "Performance.jsonc"

// performanceFileNamers is every tracked file that may name performanceFile, each with why.
var performanceFileNamers = map[string]string{
	"command/cohere-dev/land_performance.go":         "the writer",
	"command/cohere-dev/land_performance_test.go":    "the writer's tests",
	"internal/guard/performance_file_unread_test.go": "this guard",
	"Performance.jsonc":                              "the file, naming itself in its header",
	"CONTRIBUTING.md":                                "the house's account of land, which says land alone writes it",
}

// Nothing that builds or tests reads Performance.jsonc (#6wnsnq2, ruling 2). cohere-dev land writes it into each
// landing commit after the gate has passed, from Shackler's measurement of the gated commit, so the tree that lands is
// the gated tree with that one file changed. A package or a test that read it would run, on main, on bytes no gate ran
// it on, and what was gated would no longer be what landed.
//
// It greps every file git tracks for the name, and every file it would track, so a file not yet committed is read
// too. The writer and its tests name it, and so do the few files that build and test nothing: the file itself, in its
// header, `CONTRIBUTING.md`, which says land alone writes it, and this guard. Any other file naming it is refused, by
// path.
func TestNothingThatBuildsOrTestsReadsPerformanceJsonc(test *testing.T) {
	test.Parallel()
	module, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	named := trackedFilesNaming(test, module, performanceFile)
	// A grep that found not even the writer looked at no tree, so a clean answer would mean nothing.
	if !slices.Contains(named, "command/cohere-dev/land_performance.go") {
		test.Fatalf("git grep found %s named in %v, not even in its writer, so it read no tree", performanceFile, named)
	}
	refused := []string{}
	for _, path := range named {
		if _, allowed := performanceFileNamers[path]; !allowed {
			refused = append(refused, path)
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		test.Errorf("%d files name %s, which land writes after the gate, so nothing that builds or tests may read it "+
			"(#6wnsnq2): what was gated would no longer be what lands. Read what it measures from where it is "+
			"measured, never from the file:\n  %s", len(refused), performanceFile, strings.Join(refused, "\n  "))
	}
}

// trackedFilesNaming is every file git tracks in the repository at root, or would track, untracked and not ignored,
// its submodules left out, whose contents hold text, by its path from the root.
func trackedFilesNaming(test *testing.T, root string, text string) []string {
	test.Helper()
	grep := exec.Command("git", "grep", "--untracked", "-l", "-z", "-F", "-e", text, "--", ".")
	// The repository's own grep alone: no one's settings, so a test's git never reads the machine's configuration.
	grep.Dir, grep.Env = root, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	var standardError bytes.Buffer
	grep.Stderr = &standardError
	output, grepError := grep.Output()
	// git grep exits 1 when nothing matches, which is an answer, and anything else is a failure to read.
	var exited *exec.ExitError
	if grepError != nil && !(errors.As(grepError, &exited) && exited.ExitCode() == 1 && standardError.Len() == 0) {
		test.Fatalf("git grep for %s in %s: %v\n%s", text, root, grepError, standardError.String())
	}
	named := []string{}
	for _, path := range strings.Split(string(output), "\x00") {
		if path != "" {
			named = append(named, path)
		}
	}
	return named
}
