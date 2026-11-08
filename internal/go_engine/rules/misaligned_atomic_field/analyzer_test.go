package misaligned_atomic_field

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/staticcheck/sa1027"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// platform is a target the fixtures are loaded for, whose sizes the check reads.
type platform struct {
	// architecture is the platform's `GOARCH`.
	architecture string
	// judged is whether the platform's pointers are 4 bytes, the only platforms the check judges.
	judged bool
}

// platforms are the targets every test but the differences' loads the fixtures for: two 32-bit platforms, where a
// 64-bit word's alignment is the caller's to keep, and two 64-bit ones, where it is not and the check finds nothing.
// Each is a Linux target, the one system every one of these architectures builds for.
var platforms = []platform{
	{architecture: "386", judged: true},
	{architecture: "arm", judged: true},
	{architecture: "amd64", judged: false},
	{architecture: "arm64", judged: false},
}

// staticcheck's own fixture for SA1027, copied from staticcheck/sa1027/testdata/go1.0/CheckAtomicAlignment into
// testdata/src/CheckAtomicAlignment unchanged but for the `//go:build` line gofmt writes above each file's
// `// +build` line, the same constraint, which the house's gofmt guard asks of every fixture: every `//@ diag(...)`
// note the original expects, the port reports on the same line with a message the note matches, and the port reports
// nothing no note expects, as staticcheck_wants matches them. The fixture is two files, each constrained to the
// 32-bit or the 64-bit platforms, and staticcheck runs it on the machine's own, so the test runs it on each of
// platforms, wanting the notes of the files built there alone: the 32-bit file's on a 32-bit platform, and none on a
// 64-bit one, whose file holds the same calls unnoted. The go1.0 folder sets the language version of staticcheck's
// run, which no part of SA1027 reads, so the fixture loads from a tree laid out by import path as the house's
// fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wants, readError := staticcheck_wants.Read(filepath.Join(analysistest.TestData(), "src", "CheckAtomicAlignment"))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	for _, target := range platforms {
		test.Run(target.architecture, func(test *testing.T) {
			test.Parallel()
			loaded, graph := analyze(test, target, []*analysis.Analyzer{Analyzer}, "CheckAtomicAlignment")
			built := map[string]bool{}
			for _, loadedPackage := range loaded {
				for _, file := range loadedPackage.CompiledGoFiles {
					built[file] = true
				}
			}
			platformWants := []staticcheck_wants.Want{}
			for _, want := range wants {
				if built[want.File] {
					platformWants = append(platformWants, want)
				}
			}
			if target.judged && len(platformWants) == 0 {
				test.Fatal("no noted file of the upstream fixture was built, so the comparison proves nothing")
			}
			findings := []staticcheck_wants.Finding{}
			for _, action := range graph.Roots {
				if action.Err != nil {
					test.Fatalf("running %s: %v", action, action.Err)
				}
				for _, diagnostic := range action.Diagnostics {
					position := action.Package.Fset.Position(diagnostic.Pos)
					findings = append(findings, staticcheck_wants.Finding{
						File:    position.Filename,
						Line:    position.Line,
						Message: diagnostic.Message,
					})
				}
			}
			test.Logf("the fixture wants %d, the port reported %d", len(platformWants), len(findings))
			unexpected, untaken := staticcheck_wants.Compare(platformWants, findings)
			for _, found := range unexpected {
				test.Errorf("%s:%d: the port reported %q, which no note on the line expects",
					filepath.Base(found.File), found.Line, found.Message)
			}
			for _, want := range untaken {
				test.Errorf("the port reported nothing matching %s", want)
			}
		})
	}
}

// The original and the port run in one checker over the same fixture packages, staticcheck's and parity's, the
// second holding the shapes staticcheck's representation builds a field's address or a call in more than one way,
// on each of platforms, and their findings must be the same multiset of file, start, end and message. Each side's
// count is logged, since two empty sides would match and prove nothing: on a 32-bit platform the test fails if the
// original finds nothing, and on a 64-bit one if it finds anything, which would mean the fixtures were not checked
// for the platform named.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, target := range platforms {
		test.Run(target.architecture, func(test *testing.T) {
			test.Parallel()
			original, port := findingsOf(test, target, "CheckAtomicAlignment", "parity")
			test.Logf("the original found %d, the port %d", len(original), len(port))
			if target.judged && len(original) == 0 {
				test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
			}
			if !target.judged && len(original) > 0 {
				test.Fatalf("the original found %d on a 64-bit platform, so the fixtures were not checked for it",
					len(original))
			}
			for _, missing := range difference(original, port) {
				test.Errorf("the original found %s, and the port did not", described(missing))
			}
			for _, extra := range difference(port, original) {
				test.Errorf("the port found %s, and the original did not", described(extra))
			}
		})
	}
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture, on a 32-bit platform, the two may disagree only where each disagreement was
// adjudicated in the port's favour: every finding the port alone makes is recorded on its line, as found or as a
// renaming, every finding the original alone makes is the original's side of a recorded renaming, and every record
// is matched. A finding the original makes and the port does not, unrecorded, fails, since the port is never worse
// than the original. The fixture holds the calls where staticcheck_call takes another path than callcheck and
// records no difference, since there is none, so the test holds that the two agree there, and fails should the
// original find nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, platforms[0], "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
	expectedPort, expectedOriginal := recorded(test, "differences")
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
}

// In a generic function's own body, a field placed after one a type parameter sizes has no offset, and go/types
// panics when the original asks for one on a 32-bit platform, in a goroutine of the checker the test cannot recover,
// so the original does not run over the type_parameters fixture; the panic was seen in Offsetsof, at go/types'
// gcsizes.go assertion that Alignof is never asked of a type parameter. The port runs alone there, on a 32-bit
// platform, and must find exactly what the fixture records: nothing at those fields, and the misaligned field of a
// plain struct in the same body, which proves it judged the body at all.
func TestPortJudgesNothingWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	_, graph := analyze(test, platforms[0], []*analysis.Analyzer{Analyzer}, "type_parameters")
	port := []finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		if action.Analyzer != Analyzer {
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			port = append(port, finding{
				file:    filepath.Base(position.Filename),
				line:    position.Line,
				message: diagnostic.Message,
			})
		}
	}
	expectedPort, expectedOriginal := recorded(test, "type_parameters")
	test.Logf("the port found %d, the fixture records %d", len(port), len(expectedPort))
	if len(expectedPort) == 0 || len(expectedOriginal) > 0 {
		test.Fatal("the fixture must record what the port alone finds, and no renaming, since the original never runs")
	}
	compareAdjudicated(test, "the port", port, expectedPort)
}

// recorded is what the comments of the fixture package name records, its one file named for it: the findings the
// port alone makes, and the original's side of each renaming.
func recorded(test *testing.T, name string) (expectedPort []finding, expectedOriginal []finding) {
	test.Helper()
	expectedPort = []finding{}
	expectedOriginal = []finding{}
	base := name + ".go"
	file, openError := os.Open(filepath.Join(analysistest.TestData(), "src", name, base))
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", base, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: base, line: line, message: match[1]})
			default:
				expectedOriginal = append(expectedOriginal, finding{file: base, line: line, message: match[2]})
				expectedPort = append(expectedPort, finding{file: base, line: line, message: match[3]})
			}
		}
	}
	if scanError := fileScanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return expectedPort, expectedOriginal
}

// compareAdjudicated fails the test unless the findings side alone makes are exactly those the fixture records.
func compareAdjudicated(test *testing.T, side string, actual []finding, expected []finding) {
	test.Helper()
	for _, unrecorded := range difference(actual, expected) {
		test.Errorf("%s alone found %s, which the fixture does not record", side, described(unrecorded))
	}
	for _, unmatched := range difference(expected, actual) {
		test.Errorf("the fixture records that %s alone finds %s, and it does not", side, described(unmatched))
	}
}

// finding is one finding as the differential compares it, its range from start to end.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it, with its end.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// analyze loads the fixture packages patterns name, checked for target, and runs analyzers over them in one checker.
// A root package that does not type-check fails the test, since the checker skips it and would compare nothing.
func analyze(
	test *testing.T,
	target platform,
	analyzers []*analysis.Analyzer,
	patterns ...string,
) ([]*packages.Package, *checker.Graph) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one, for the target, with cgo
	// off as it is by default for a target other than the machine's.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
		"GOOS=linux", "GOARCH=" + target.architecture, "CGO_ENABLED=0",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	return loaded.Roots, graph
}

// findingsOf runs staticcheck's SA1027 and the port over the fixture packages patterns name, checked for target, in
// one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, target platform, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	_, graph := analyze(test, target, []*analysis.Analyzer{sa1027.Analyzer, Analyzer}, patterns...)
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			switch action.Analyzer {
			case sa1027.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []finding, without []finding) []finding {
	remaining := map[finding]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []finding{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// withoutColumns is findings with their columns and ends dropped, for comparing with the differences fixture's
// comments, which name a line.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		dropped = append(dropped, finding{file: found.file, line: found.line, message: found.message})
	}
	return dropped
}
