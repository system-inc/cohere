package unreachable

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for unreachable, a.go and its golden file copied unchanged into testdata/src/a, run as x/tools'
// test runs it, with no flags and with its suggested fixes: every `// want` the original expects, the port reports at
// the same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, so the port adds nothing there either, and every fix the port offers, applied, turns a.go into its golden
// file. The parity fixtures' wants are held to the port the same way, so each says truly what it holds.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "parity", "parity/shadowed")
	parity.RequireUpstreamWants(test, Analyzer, fixtureTree(test), "layout")
}

// aMissingReturnLines are the 209 lines of x/tools' a.go where a function whose result is an int ends with no return,
// each its closing brace, at column 1.
var aMissingReturnLines = []int{
	18, 22, 28, 35, 41, 49, 58, 66, 76, 85, 100, 108, 115, 123, 130, 139, 149, 158, 167, 177, 186, 196, 207, 222,
	235, 243, 256, 263, 272, 300, 308, 317, 327, 334, 343, 353, 360, 369, 379, 385, 395, 405, 416, 427, 442, 456,
	469, 481, 490, 504, 516, 525, 536, 549, 563, 576, 589, 601, 614, 626, 638, 650, 657, 669, 681, 693, 703, 717,
	730, 745, 758, 770, 783, 795, 807, 819, 826, 838, 850, 862, 872, 886, 899, 914, 921, 926, 932, 937, 944, 951,
	958, 966, 974, 981, 988, 995, 1003, 1010, 1019, 1029, 1038, 1047, 1057, 1066, 1071, 1075, 1081, 1088, 1094,
	1102, 1111, 1119, 1129, 1138, 1153, 1161, 1168, 1176, 1183, 1192, 1202, 1211, 1220, 1230, 1239, 1249, 1260,
	1275, 1288, 1296, 1309, 1316, 1325, 1353, 1361, 1370, 1380, 1387, 1396, 1406, 1413, 1422, 1432, 1438, 1448,
	1458, 1469, 1480, 1495, 1509, 1522, 1534, 1543, 1557, 1569, 1578, 1589, 1602, 1616, 1629, 1642, 1654, 1667,
	1679, 1691, 1703, 1710, 1722, 1734, 1746, 1756, 1770, 1783, 1798, 1811, 1823, 1836, 1848, 1860, 1872, 1879,
	1891, 1903, 1915, 1925, 1939, 1952, 1967, 1974, 1979, 1985, 1990, 1997, 2004, 2011, 2019, 2027, 2034, 2041,
	2048, 2056, 2063, 2072, 2082, 2091, 2100, 2110, 2119, 2128,
}

// missingReturnsInA is the load error a.go reports at each of lines, a function's closing brace with no return before
// it.
func missingReturnsInA(lines []int) []parity.LoadError {
	loadErrors := []parity.LoadError{}
	for _, line := range lines {
		loadErrors = append(loadErrors, parity.LoadError{
			Position: fmt.Sprintf("a/a.go:%d:1", line),
			Message:  "missing return",
		})
	}
	return loadErrors
}

// illTypedLoadErrors are the 220 errors the fixture packages that do not type-check on purpose load with, each listed:
// x/tools' a, whose file says it "produces masses of errors from the type checker due to missing returns statements
// and other things", 218 of them, and truncated, a file of layout.txtar cut off in the middle of a function, which
// does not parse, 2.
var illTypedLoadErrors = slices.Concat(missingReturnsInA(aMissingReturnLines), []parity.LoadError{
	// A label L that no goto names, before an if whose every way through ends but one.
	{Position: "a/a.go:246:1", Message: "label L declared and not used"},
	{Position: "a/a.go:1299:1", Message: "label L declared and not used"},
	// A fallthrough in a type switch's clause, a way through the switch the walk follows as written.
	{Position: "a/a.go:802:3", Message: "cannot fallthrough in type switch"},
	{Position: "a/a.go:814:3", Message: "cannot fallthrough in type switch"},
	{Position: "a/a.go:857:3", Message: "cannot fallthrough in type switch"},
	{Position: "a/a.go:1855:3", Message: "cannot fallthrough in type switch"},
	{Position: "a/a.go:1867:3", Message: "cannot fallthrough in type switch"},
	{Position: "a/a.go:1910:3", Message: "cannot fallthrough in type switch"},
	// A switch with no tag and a case value that is not a bool, a shape the fixture says once panicked.
	{Position: "a/a.go:2124:7", Message: "cannot convert 1 (untyped int constant) to type bool"},
	// truncated's one file ends inside a function, after its dead statement.
	{Position: "truncated/truncated.go:5:18", Message: "expected ';', found 'EOF'"},
	{Position: "truncated/truncated.go:5:18", Message: "expected '}', found 'EOF'"},
})

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the walk and the shapes most likely to differ, and the layouts' (testdata/layout.txtar),
// every way the statement deleted can share its lines, and their findings must be the same multiset, each the same in
// every field, its fixes edit for edit, each edit as its offsets and new text, which a golden file, gofmt'd before it
// is compared, would not hold to the character. Two of the packages do not type-check on purpose, and both sides run
// despite errors, so their load errors are listed exactly.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalDespiteLoadErrors(test, unreachable.Analyzer, Analyzer, fixtureTree(test),
		illTypedLoadErrors, "a", "parity", "parity/shadowed", "layout", "truncated")
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour, as the fixture records it on the finding's line, with the reason on the line above: every finding the
// original alone makes there is dropped, wrong where the package declares a panic of its own, which returns.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, unreachable.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// runWithoutTypes runs the port over checked's syntax alone: first every analyzer it requires, the syntax dispatcher
// and what the dispatcher requires, each once and over the same syntax, each handed the results of what it requires,
// then the port's own analyzer, which reports what the dispatcher's walk found through checked's report, the
// checker's, which holds every fix to be well formed, and cuts an edit's end just past the file's, as the deletion in
// a file cut off makes, back to the file's end. A pass here holds no types, package or type checker's information,
// so an analyzer that read any would find nothing there or fail.
func runWithoutTypes(checked *analysis.Pass) (any, error) {
	// results is each analyzer's result once it has run, as a pass's ResultOf holds them.
	results := map[*analysis.Analyzer]any{}
	var run func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic)) error
	run = func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic)) error {
		resultOf := map[*analysis.Analyzer]any{}
		for _, required := range running.Requires {
			if _, isRun := results[required]; !isRun {
				runError := run(required, func(analysis.Diagnostic) {
					// A requirement's findings are not the port's, and none of these makes any.
				})
				if runError != nil {
					return fmt.Errorf("running %s with no types: %w", required.Name, runError)
				}
			}
			resultOf[required] = results[required]
		}
		result, runError := running.Run(&analysis.Pass{
			Analyzer: running,
			Fset:     checked.Fset,
			Files:    checked.Files,
			ResultOf: resultOf,
			Report:   report,
		})
		results[running] = result
		return runError
	}
	if runError := run(Analyzer, checked.Report); runError != nil {
		return nil, runError
	}
	return results[Analyzer], nil
}

// typeless is the port run with no type information (see runWithoutTypes). It runs despite errors as the port does,
// and requires nothing, so the checker hands it every package and runs nothing for it.
var typeless = &analysis.Analyzer{
	Name:             Analyzer.Name,
	Doc:              "typeless runs the port with no type information.",
	RunDespiteErrors: true,
	Run:              runWithoutTypes,
}

// The registry lists unreachable among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over every fixture package, the differences' too, once with types and once as typeless,
// in one checker, and must find with none what it finds with them, each finding the same in every field, fix for
// fix. The harness names the side with types the original.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalDespiteLoadErrors(test, Analyzer, typeless, fixtureTree(test), illTypedLoadErrors,
		"a", "parity", "parity/shadowed", "differences", "layout", "truncated")
}

// fixtureTree is a `GOPATH` of the test's own, so every fixture package loads in one call: testdata/src copied file for
// file, and testdata/layout.txtar extracted beside it, since cohere's gofmt guard reads every Go file under testdata
// and truncated does not parse.
func fixtureTree(test *testing.T) string {
	test.Helper()
	tree := test.TempDir()
	write := func(name string, data []byte) {
		path := filepath.Join(tree, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	walkError := filepath.WalkDir(filepath.Join(analysistest.TestData(), "src"),
		func(path string, entry fs.DirEntry, entryError error) error {
			if entryError != nil || entry.IsDir() {
				return entryError
			}
			data, readError := os.ReadFile(path)
			if readError != nil {
				return readError
			}
			below, relativeError := filepath.Rel(analysistest.TestData(), path)
			if relativeError != nil {
				return relativeError
			}
			write(filepath.ToSlash(below), data)
			return nil
		})
	if walkError != nil {
		test.Fatalf("copying testdata/src: %v", walkError)
	}
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "layout.txtar"))
	if parseError != nil {
		test.Fatalf("reading layout.txtar: %v", parseError)
	}
	for _, file := range archive.Files {
		write(file.Name, file.Data)
	}
	return tree
}
