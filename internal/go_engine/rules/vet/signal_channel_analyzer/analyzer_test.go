package signal_channel_analyzer

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/passes/sigchanyzer"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for sigchanyzer, a.go and its golden a.go.golden, copied unchanged into testdata/src and run as
// x/tools' test runs it, with suggested fixes and no flags: every `// want` the original expects, the port reports at
// the same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, and every fix the port offers, applied, turns a.go into the golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and of the fix and the shapes
// most likely to differ, the original and the port find the same findings, each the same in every field, its
// suggested fixes among them.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sigchanyzer.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
}

// differencesFixture is the differences fixture's package, under testdata's src.
const differencesFixture = "differences"

// fixRecord opens a comment in the differences fixture that records the text one fix of a finding only the port
// makes puts in place of what starts on the comment's line. It is not a `// port ` record, which the harness reads:
// the harness's `finds:` pairs a finding by line and message alone, and holds nothing of its fix.
const fixRecord = "// fix: "

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour, each record with its reason on the line above it or in its function's doc. The harness pairs the findings,
// and holds nothing of the fixes of a finding only the port makes, so this tier then holds those itself: every edit
// of them is recorded on the line it starts, as the text it puts there, and every record is matched by one.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	testdata := analysistest.TestData()
	parity.RequireOnlyAdjudicated(test, sigchanyzer.Analyzer, Analyzer, testdata, differencesFixture)
	original, port := parity.Findings(test, sigchanyzer.Analyzer, Analyzer, testdata, differencesFixture)
	// root is the fixtures' root, testdata's src, below which the harness names every file it reports, as
	// `differences/declarations.go`.
	root := filepath.Join(testdata, "src")
	editsAlone := []parity.Finding{}
	for _, found := range parity.Difference(port, original) {
		for _, fix := range found.Fixes {
			for _, edit := range fix.Edits {
				editsAlone = append(editsAlone, editAt(test, root, edit))
			}
		}
	}
	recorded := fixRecords(test, root, filepath.Join(root, differencesFixture))
	if len(recorded) == 0 {
		test.Fatalf("the differences fixture records no fix of the port's, so the comparison proves nothing")
	}
	for _, unrecorded := range parity.Difference(editsAlone, recorded) {
		test.Errorf("the port alone fixes %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range parity.Difference(recorded, editsAlone) {
		test.Errorf("the fixture records that the port alone fixes %s, and it does not", unmatched)
	}
}

// editAt is edit as a fix record names it, a finding with the edit's file, the line its start is on and the text it
// puts there as the message, so the harness's Difference compares edits with records. edit's file is its path below
// root, as the harness names it.
func editAt(test *testing.T, root string, edit parity.Edit) parity.Finding {
	test.Helper()
	content, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(edit.File)))
	if readError != nil {
		test.Fatalf("reading the file a fix edits: %v", readError)
	}
	if edit.Start > len(content) {
		test.Fatalf("a fix edits %s at offset %d, past its end", edit.File, edit.Start)
	}
	line := 1 + bytes.Count(content[:edit.Start], []byte("\n"))
	return parity.Finding{File: edit.File, Line: line, Message: edit.NewText}
}

// fixRecords is every fix record of the .go files in directory, each a finding with the comment's file, named by its
// path below root as the harness names it, its line, and the recorded text as its message. It reads comments as the
// parser finds them, as the harness reads its records.
func fixRecords(test *testing.T, root string, directory string) []parity.Finding {
	test.Helper()
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing the differences fixture %s: %v", directory, globError)
	}
	recorded := []parity.Finding{}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing the differences fixture: %v", parseError)
		}
		below, relativeError := filepath.Rel(root, name)
		if relativeError != nil {
			test.Fatalf("naming %s below the fixtures' root %s: %v", name, root, relativeError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				text, isRecord := strings.CutPrefix(comment.Text, fixRecord)
				if !isRecord {
					continue
				}
				line := fileSet.Position(comment.Slash).Line
				recorded = append(recorded, parity.Finding{File: filepath.ToSlash(below), Line: line, Message: text})
			}
		}
	}
	return recorded
}
