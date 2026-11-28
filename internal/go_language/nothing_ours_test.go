package go_language

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/git_index_testing"
)

// vendoredModuleFiles are a module go refuses to list (#td4wp62): its go.mod requires a module its vendor directory
// never names, and a go.mod at go 1.14 or later with a vendor directory beside it is listed under `-mod=vendor`, so
// go list answers "inconsistent vendoring", as it did at a module run under an inherited `GOFLAGS=-mod=vendor`. The
// vendor directory stands in for the variable, since a parallel test can set no environment of the process.
var vendoredModuleFiles = map[string]string{
	"go.mod":             "module example.com/bench\n\ngo 1.27\n\nrequire golang.org/x/mod v0.20.0\n",
	"vendor/modules.txt": "",
}

// writeVendoredModule writes vendoredModuleFiles with extra beside them in a repository of its own, whose index tracks
// every file it wrote but untracked, written without git (git_index_testing), and returns its root, every link above it
// resolved. A root with no Go file of ours is skipped only when its index says so too (nothingToList), so each
// repository here has one.
func writeVendoredModule(test *testing.T, extra map[string]string, untracked ...string) string {
	test.Helper()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	files := maps.Clone(vendoredModuleFiles)
	maps.Copy(files, extra)
	writeTreeAt(test, root, files, ".")
	tracked := slices.DeleteFunc(slices.Collect(maps.Keys(files)), func(name string) bool {
		return slices.Contains(untracked, name)
	})
	if writeError := git_index_testing.Write(filepath.Join(root, ".git"), tracked...); writeError != nil {
		test.Fatalf("writing the index: %v", writeError)
	}
	return root
}

// benchFile is one Go file of the vendored module's package, by name, with its text.
var benchFile = map[string]string{"bench.go": "// Package bench is one.\npackage bench\n"}

// listingRefused is the types phase's record of a load go refused, as the vendored module's is.
const listingRefused = `"name":"types","outcome":"notReached","elapsedMilliseconds":0,"detail":"the load failed`

// A module that holds no Go file of ours is never listed (#td4wp62): there is nothing in it to check, so a tool error
// go meets only because it was asked never fails the run. Over a go.mod go refuses to list, with no Go file, the run
// exits clean and complete, its types and lint phases ran over no file, as a run over an empty module go lists
// reports them, and nothing it noted is go's refusal. It writes no inputs record, so the front door records nothing to
// replay, and a Go file added later is checked by a run of its own.
func TestAModuleWithNoGoFileOfOursIsNeverListed(test *testing.T) {
	test.Parallel()

	_, stream, noted := runOverNoted(test, writeVendoredModule(test, nil), exitClean, nil, "--no-fix")
	for _, want := range []string{
		`"filesInScope":0`,
		`"build":"nothing listed: the module holds no Go file of ours, so go list never ran"`,
		`"name":"types","outcome":"ran"`,
		`"name":"lint","outcome":"ran"`,
		`"complete":true`,
	} {
		if !strings.Contains(stream, want) {
			test.Errorf("want %s in the stream:\n%s", want, stream)
		}
	}
	if strings.Contains(stream, `{"kind":"inputs"`) {
		test.Errorf("want no inputs record, so nothing is replayed over a module that later holds a file:\n%s", stream)
	}
	if strings.Contains(noted, "inconsistent vendoring") {
		test.Errorf("want go's refusal unread, got\n%s", noted)
	}
}

// A module whose only Go file git ignores holds none of ours, and is never listed either (#td4wp62): the file is
// named excluded, as any ignored file is, and the run is clean and complete.
func TestAModuleWhoseOnlyGoFileGitIgnoresIsNeverListed(test *testing.T) {
	test.Parallel()

	root := writeVendoredModule(test, map[string]string{
		".gitignore": "ignored.go\n",
		"ignored.go": "// Package bench is ignored.\npackage bench\n",
	}, "ignored.go")
	_, stream, noted := runOverNoted(test, root, exitClean, nil, "--no-fix")
	if !strings.Contains(stream, `"file":"`+filepath.Join(root, "ignored.go")+`","reason":"ignored by git"`) ||
		!strings.Contains(stream, `"complete":true`) {
		test.Errorf("want ignored.go excluded and the run complete:\n%s", stream)
	}
	if strings.Contains(noted, "inconsistent vendoring") {
		test.Errorf("want go's refusal unread, got\n%s", noted)
	}
}

// A module with a Go file of ours is listed, and a listing go refuses still fails the run (#td4wp62): the skip of a
// module with nothing to check hides no failure of one with something. Over the same go.mod with one file, the types
// phase is not reached, saying go's refusal, and the run is incomplete and exits 1.
func TestAModuleWithAGoFileWhoseListingFailsStillFails(test *testing.T) {
	test.Parallel()

	_, stream, noted := runOverNoted(test, writeVendoredModule(test, benchFile), exitFindings, nil, "--no-fix")
	if !strings.Contains(stream, listingRefused) || !strings.Contains(stream, "inconsistent vendoring") ||
		!strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the types phase not reached on go's refusal and the run incomplete:\n%s", stream)
	}
	if !strings.Contains(noted, "inconsistent vendoring") {
		test.Errorf("want go's refusal noted, got\n%s", noted)
	}
}

// A census that counts nothing is never trusted alone (#td4wp62): the index of the module's repository is a second
// witness, and where it tracks a Go file go reads, the root is listed as any root with files is, and the run says the
// two disagree as cohere's bug. Over the module with bench.go, tracked, and its census emptied as a walk that never
// reached it would leave it (emptyCensus), the run lists, go's refusal fails it, and the note names bench.go.
func TestACensusOfNothingOverATrackedGoFileIsListedAndSaid(test *testing.T) {
	test.Parallel()

	root := writeVendoredModule(test, benchFile)
	_, stream, noted := runOverNoted(test, root, exitFindings, emptyCensus, "--no-fix")
	if !strings.Contains(stream, listingRefused) || !strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the module listed, go's refusal failing the types phase, and the run incomplete:\n%s", stream)
	}
	if !strings.Contains(noted, "the repository's index tracks "+filepath.Join(root, "bench.go")) ||
		!strings.Contains(noted, "a bug in cohere") {
		test.Errorf("want the disagreement said as cohere's bug, naming bench.go, got\n%s", noted)
	}
}

// With no index to read there is no second witness, so a root the census counts nothing in is listed as it always was
// (#td4wp62): over the module with no Go file in a repository with no index yet, as a fresh `git init` leaves it, the
// run lists, and go's refusal fails it.
func TestAModuleWithNoGoFileAndNoIndexIsListed(test *testing.T) {
	test.Parallel()

	root := writeVendoredModule(test, nil)
	if removeError := os.Remove(filepath.Join(root, ".git", "index")); removeError != nil {
		test.Fatal(removeError)
	}
	_, stream, _ := runOverNoted(test, root, exitFindings, nil, "--no-fix")
	if !strings.Contains(stream, listingRefused) || !strings.Contains(stream, "inconsistent vendoring") {
		test.Errorf("want the module listed and go's refusal failing the types phase:\n%s", stream)
	}
}
