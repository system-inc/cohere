package time_format

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for timeformat, copied unchanged into testdata/src, a.go beside its golden file and the b it
// imports, and run as x/tools' test runs them, with suggested fixes and no flags: every `// want` the original
// expects, the port reports at the same line with the same message, analysistest fails on a finding no want expects as
// much as on a want nothing found, and the port's fixes, applied, turn a.go into a.go.golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixtures and those written for the port, parity holding every branch of the judgment and the shapes
// most likely to differ and dotted a dot import, the original and the port find the same findings, each the same in
// every field, each fix's edits compared as offsets and text, since a golden file is gofmt'd first and would miss an
// edit off by one byte.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, timeformat.Analyzer, Analyzer, analysistest.TestData(), "a", "parity", "dotted")
}

// Over the differences fixtures the two disagree, and only where each disagreement was adjudicated in the port's
// favour, as the fixtures record it on the finding's line, with the reason on the line above: a move is a finding
// both make, the original's over the wrong bytes, and a find a true finding only the port makes. Each record places
// the port's finding, its column and its end, so a finding that has an end spans the bytes that spell the mistake on
// its line, and the find at a named constant, which has none, sits at the name, `finds at column 31, no end:`. Each
// names its fix's message, and a find's `fixes:` record the text its one edit writes, so the find at the named
// constant, with no `fixes:` record, offers no fix, with edits or without. Beside each fixture file is its `.fixed`
// file, the file as every fix the port offers in it leaves it, byte for byte, so each fix's bytes are held, and each
// move proves the original's fix wrong, since the original's fix, in the port's place, must not make it. The
// differences package is gofmt-clean and lives in testdata/src. The carriage returns of carriage.go are not, since
// gofmt drops a raw literal's, so it lives with its `.fixed` file in carriage.txtar and is extracted into a directory
// of the test's own.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, timeformat.Analyzer, Analyzer, analysistest.TestData(), "differences")
	parity.RequireOnlyAdjudicated(test, timeformat.Analyzer, Analyzer, extractedCarriage(test), "carriage")
}

// extractedCarriage writes carriage.txtar's files, carriage returns and all, into a directory of the test's own laid
// out as testdata is, and returns it.
func extractedCarriage(test *testing.T) string {
	test.Helper()
	archive, archiveError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "carriage.txtar"))
	if archiveError != nil {
		test.Fatal(archiveError)
	}
	extracted := test.TempDir()
	for _, file := range archive.Files {
		path := filepath.Join(extracted, filepath.FromSlash(file.Name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, file.Data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return extracted
}
