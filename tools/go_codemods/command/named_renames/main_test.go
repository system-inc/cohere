package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

func writeFixture(test *testing.T, files map[string]string) string {
	root := test.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

func readFixture(test *testing.T, root string, name string) string {
	contents, readError := os.ReadFile(filepath.Join(root, name))
	if readError != nil {
		test.Fatal(readError)
	}
	return string(contents)
}

// A chosen rename renames its declaration and every reader, in its package and in another, and its own doc
// comment; a declaration of the same name in another file, which no line names, keeps its name, as does a
// comment that is not the declaration's own. The library is pinned: unpinned, its exports are refused.
func TestNamedRenamesRenameTheChosenDeclarationOnly(test *testing.T) {
	test.Parallel()
	root := writeFixture(test, map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"single/verify.go": `package single

// SSAStats counts what was checked.
type SSAStats struct{ Uses int }

// CollectSSAStats measures one function.
func CollectSSAStats() SSAStats { return SSAStats{} }
`,
		"reader/reader.go": `package reader

import "fixture/single"

// SSAStats is this package's own, which no line names.
type SSAStats struct{ Phis int }

// Collect returns what CollectSSAStats measures.
func Collect() (single.SSAStats, SSAStats) { return single.CollectSSAStats(), SSAStats{} }
`,
	})
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	proposals, unmatched := findProposals(loaded, []chosenRename{
		{File: "single/verify.go", OldName: "SSAStats", NewName: "StaticSingleAssignmentStatistics"},
		{File: "single/verify.go", OldName: "CollectSSAStats", NewName: "CollectStaticSingleAssignmentStatistics"},
	})
	if len(unmatched) != 0 || len(proposals) != 2 {
		test.Fatalf("proposed %d, unmatched %v; want 2 and none", len(proposals), unmatched)
	}
	if unpinned := global_rename.Plan(loaded, proposals, global_rename.Options{}); len(unpinned.Rejections) != 2 ||
		unpinned.Rejections[0].Class != "exported outside the workspace" {
		test.Fatalf("unpinned, refused %v; want both refused as exported outside the workspace", unpinned.Rejections)
	}
	result := global_rename.Plan(
		loaded,
		proposals,
		global_rename.Options{PinnedLibraries: map[string]string{"fixture/single": "read at a pin"}},
	)
	if len(result.Rejections) != 0 {
		test.Fatalf("refused %v", result.Rejections)
	}
	if mismatches, applyError := global_rename.Apply(
		loaded,
		root,
		nil,
		result,
		bufio.NewWriter(io.Discard),
	); applyError != nil || mismatches != 0 {
		test.Fatalf("apply: %d mismatches, %v", mismatches, applyError)
	}
	wantVerify := `package single

// StaticSingleAssignmentStatistics counts what was checked.
type StaticSingleAssignmentStatistics struct{ Uses int }

// CollectStaticSingleAssignmentStatistics measures one function.
func CollectStaticSingleAssignmentStatistics() StaticSingleAssignmentStatistics {
	return StaticSingleAssignmentStatistics{}
}
`
	if got := readFixture(test, root, "single/verify.go"); got != wantVerify {
		test.Errorf("single/verify.go:\n%s\nwant:\n%s", got, wantVerify)
	}
	wantReader := `package reader

import "fixture/single"

// SSAStats is this package's own, which no line names.
type SSAStats struct{ Phis int }

// Collect returns what CollectSSAStats measures.
func Collect() (single.StaticSingleAssignmentStatistics, SSAStats) {
	return single.CollectStaticSingleAssignmentStatistics(), SSAStats{}
}
`
	if got := readFixture(test, root, "reader/reader.go"); got != wantReader {
		test.Errorf("reader/reader.go:\n%s\nwant:\n%s", got, wantReader)
	}
}

// A line that names no declaration in its file, or two, proposes nothing and says why.
func TestNamedRenamesLeaveAnUnmatchedLine(test *testing.T) {
	test.Parallel()
	root := writeFixture(test, map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"single/single.go": `package single

func first() int { stats := 1; return stats }

func second() int { stats := 2; return stats }
`,
	})
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	proposals, unmatched := findProposals(loaded, []chosenRename{
		{File: "single/single.go", OldName: "stats", NewName: "statistics"},
		{File: "single/other.go", OldName: "first", NewName: "firstValue"},
	})
	if len(proposals) != 0 || len(unmatched) != 2 ||
		!strings.Contains(unmatched[0], "2 declarations of that name in that file, on lines 3, 5") ||
		!strings.Contains(unmatched[1], "no declaration of that name in that file") {
		test.Fatalf("proposed %d, unmatched %q", len(proposals), unmatched)
	}
}

// The renames file is three tab-separated fields a line, with comments and blank lines skipped.
func TestReadChosenRefusesAMalformedLine(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	good := filepath.Join(directory, "good.tsv")
	if writeError := os.WriteFile(good, []byte("# chosen\n\na.go\tOld\tNew\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	chosen, readError := readChosen(good)
	if readError != nil ||
		len(chosen) != 1 ||
		chosen[0] != (chosenRename{File: "a.go", OldName: "Old", NewName: "New"}) {
		test.Fatalf("read %v, %v", chosen, readError)
	}
	bad := filepath.Join(directory, "bad.tsv")
	if writeError := os.WriteFile(bad, []byte("a.go Old New\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, readError := readChosen(bad); readError == nil {
		test.Fatal("a line without tabs was read")
	}
}

// A fourth field, the declaration's line, picks one of two fields of the same name in one file, and a line no
// declaration of the name is on proposes nothing. The renames file reads it, and refuses a line that is no number.
func TestTheDeclarationLinePicksOneOfTwo(test *testing.T) {
	test.Parallel()
	root := writeFixture(test, map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"single/single.go": `package single

type built struct {
	path string
	err  error
}

type listed struct{ err error }

func failures(first built, second listed) (error, error) { return first.err, second.err }
`,
	})
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	proposals, unmatched := findProposals(loaded, []chosenRename{
		{File: "single/single.go", OldName: "err", NewName: "listError", Line: 8},
		{File: "single/single.go", OldName: "err", NewName: "nothing", Line: 7},
	})
	if len(proposals) != 1 || proposals[0].Package.Fset.Position(proposals[0].Object.Pos()).Line != 8 ||
		len(unmatched) != 1 || !strings.Contains(unmatched[0], "no declaration of that name in that file") {
		test.Fatalf(
			"proposed %d, unmatched %q; want the field on line 8 and line 7 unmatched",
			len(proposals),
			unmatched,
		)
	}
	result := global_rename.Plan(loaded, proposals, global_rename.Options{})
	if len(result.Rejections) != 0 {
		test.Fatalf("refused %v", result.Rejections)
	}
	if _, applyError := global_rename.Apply(loaded, root, nil, result, bufio.NewWriter(io.Discard)); applyError != nil {
		test.Fatal(applyError)
	}
	want := `package single

type built struct {
	path string
	err  error
}

type listed struct{ listError error }

func failures(first built, second listed) (error, error) { return first.err, second.listError }
`
	if got := readFixture(test, root, "single/single.go"); got != want {
		test.Fatalf("single/single.go:\n%s\nwant:\n%s", got, want)
	}

	directory := test.TempDir()
	withLine := filepath.Join(directory, "line.tsv")
	if writeError := os.WriteFile(withLine, []byte("a.go\terr\tlistError\t5\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	chosen, readError := readChosen(withLine)
	if readError != nil ||
		len(chosen) != 1 ||
		chosen[0] != (chosenRename{File: "a.go", OldName: "err", NewName: "listError", Line: 5}) {
		test.Fatalf("read %v, %v", chosen, readError)
	}
	notANumber := filepath.Join(directory, "word.tsv")
	if writeError := os.WriteFile(notANumber, []byte("a.go\terr\tlistError\tfive\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, readError := readChosen(notANumber); readError == nil {
		test.Fatal("a line whose fourth field is no number was read")
	}
}

// A field whose name a source-reading test in another package spells as a literal is refused, accepted once the
// -reviewed-literals file clears that file's literal, and refused again when a second such test holds it unreviewed;
// the refusal then names only the unreviewed file.
func TestReviewedLiteralsClearOnlyTheirFile(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"single/single.go": `package single

type record struct{ src string }

func Read() string { return record{src: "input"}.src }
`,
		"reader/reader.go": "package reader\n",
		"reader/reader_test.go": `package reader

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestRead(test *testing.T) {
	if _, parseError := parser.ParseDir(token.NewFileSet(), "src", nil, 0); parseError != nil {
		test.Fatal(parseError)
	}
}
`,
	}
	chosen := []chosenRename{{File: "single/single.go", OldName: "src", NewName: "source"}}
	plan := func(options global_rename.Options) *global_rename.Result {
		loaded, loadError := workspace.Load(writeFixture(test, files), nil)
		if loadError != nil {
			test.Fatal(loadError)
		}
		proposals, unmatched := findProposals(loaded, chosen)
		if len(proposals) != 1 || len(unmatched) != 0 {
			test.Fatalf("proposed %d, unmatched %q; want the field", len(proposals), unmatched)
		}
		return global_rename.Plan(loaded, proposals, options)
	}

	unreviewed, optionsError := planOptions(nil, "")
	if optionsError != nil {
		test.Fatal(optionsError)
	}
	if result := plan(unreviewed); len(result.Rejections) != 1 || result.Rejections[0].Class != "string literal" {
		test.Fatalf("unreviewed, refused %v; want the field refused for its string literal", result.Rejections)
	}

	directory := test.TempDir()
	reviewedPath := filepath.Join(directory, "reviewed.tsv")
	if writeError := os.WriteFile(
		reviewedPath,
		[]byte("# reviewed\n\nsrc\treader/reader_test.go\tthe directory the test parses, not a field\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	reviewed, optionsError := planOptions(nil, reviewedPath)
	if optionsError != nil {
		test.Fatal(optionsError)
	}
	if result := plan(reviewed); len(result.Rejections) != 0 || len(result.Accepted) != 1 {
		test.Fatalf(
			"reviewed, accepted %d and refused %v; want the field accepted",
			len(result.Accepted),
			result.Rejections,
		)
	}

	files["other/other.go"] = "package other\n"
	files["other/other_test.go"] = strings.ReplaceAll(files["reader/reader_test.go"], "package reader", "package other")
	result := plan(reviewed)
	if len(result.Rejections) != 1 ||
		!strings.Contains(result.Rejections[0].Reason, "other/other_test.go") ||
		strings.Contains(result.Rejections[0].Reason, "reader/reader_test.go") {
		test.Fatalf(
			"with a second file unreviewed, refused %v; want the field refused for other_test.go only",
			result.Rejections,
		)
	}

	malformed := filepath.Join(directory, "malformed.tsv")
	if writeError := os.WriteFile(malformed, []byte("src\treader/reader_test.go\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, readError := planOptions(nil, malformed); readError == nil {
		test.Fatal("a line without a reason was read")
	}
}
