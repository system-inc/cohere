package go_language

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// vendoredModuleFiles are a module go refuses to list (#td4wp62): its go.mod requires a module its vendor directory
// never names, and a go.mod at go 1.14 or later with a vendor directory beside it is listed under `-mod=vendor`, so
// go list answers "inconsistent vendoring", as it did at a module run under an inherited `GOFLAGS=-mod=vendor`. The
// vendor directory stands in for the variable, since a parallel test can set no environment of the process.
var vendoredModuleFiles = map[string]string{
	"go.mod":             "module example.com/bench\n\ngo 1.27\n\nrequire golang.org/x/mod v0.20.0\n",
	"vendor/modules.txt": "",
}

// writeVendoredModule writes vendoredModuleFiles with extra beside them in a repository of its own, and returns its
// root, every link above it resolved.
func writeVendoredModule(test *testing.T, extra map[string]string) string {
	test.Helper()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	files := maps.Clone(vendoredModuleFiles)
	maps.Copy(files, extra)
	writeTreeAt(test, root, files, ".")
	return root
}

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
	})
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

	root := writeVendoredModule(test, map[string]string{"bench.go": "// Package bench is one.\npackage bench\n"})
	_, stream, noted := runOverNoted(test, root, exitFindings, nil, "--no-fix")
	notReached := `"name":"types","outcome":"notReached","elapsedMilliseconds":0,"detail":"the load failed`
	if !strings.Contains(stream, notReached) || !strings.Contains(stream, "inconsistent vendoring") ||
		!strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the types phase not reached on go's refusal and the run incomplete:\n%s", stream)
	}
	if !strings.Contains(noted, "inconsistent vendoring") {
		test.Errorf("want go's refusal noted, got\n%s", noted)
	}
}
