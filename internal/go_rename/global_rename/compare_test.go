package global_rename

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// Compare reads a load of the rewrite against the expectation and returns what differs as data: nothing for the
// plan's own rewrite, and for a rewrite that also renames a local the plan never touched, each of its identifiers,
// in its file, at the offset an edit there would have, under the name it was expected to keep.
func TestCompareReportsAPlantedMismatchAsData(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type Record struct{ GroupID string }
`,
		"internal/b/b.go": `package b

import "fixture/internal/a"

func Build() string {
	record := a.Record{GroupID: "x"}
	pointer := &record
	return pointer.GroupID
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{})
	requireAccepted(test, result, 1)
	expectation := Expect(loaded, result)
	files, rewriteError := RewrittenFiles(result)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	if len(files) != 2 {
		test.Fatalf("want a.go and b.go rewritten, got %d files", len(files))
	}

	clean, cleanError := workspace.LoadWithOverlay(fixture.root, nil, files)
	if cleanError != nil {
		test.Fatal(cleanError)
	}
	if verification := Compare(expectation, clean); verification.Compared == 0 ||
		len(verification.Mismatches) != 0 {
		test.Fatalf(
			"the plan's own rewrite: %d compared, mismatches %v",
			verification.Compared,
			verification.Mismatches,
		)
	}

	importerFilename := filepath.Join(loaded.Root, "internal/b/b.go")
	original := fixture.read(test, "internal/b/b.go")
	planted := maps.Clone(files)
	planted[importerFilename] = []byte(strings.ReplaceAll(string(files[importerFilename]), "pointer", "pointed"))
	reloaded, reloadError := workspace.LoadWithOverlay(fixture.root, nil, planted)
	if reloadError != nil {
		test.Fatal(reloadError)
	}
	verification := Compare(expectation, reloaded)
	if len(verification.Mismatches) != 2 {
		test.Fatalf("want the local's declaration and its use, got %v", verification.Mismatches)
	}
	wantOffsets := []int{strings.Index(original, "pointer"), strings.LastIndex(original, "pointer")}
	for index, mismatch := range verification.Mismatches {
		if mismatch.Kind != MismatchKindIdentifier ||
			mismatch.Package != "fixture/internal/b" ||
			mismatch.Filename != importerFilename ||
			mismatch.Offset != wantOffsets[index] ||
			mismatch.Expected.Name != "pointer" ||
			mismatch.Actual.Name != "pointed" ||
			mismatch.Expected.Key != mismatch.Actual.Key ||
			!strings.Contains(mismatch.Expected.Key, "declared:"+importerFilename) {
			test.Fatalf("mismatch %d, want it at offset %d: %+v", index, wantOffsets[index], mismatch)
		}
	}
	if fixture.read(test, "internal/b/b.go") != original {
		test.Fatal("comparing wrote b.go")
	}
}
