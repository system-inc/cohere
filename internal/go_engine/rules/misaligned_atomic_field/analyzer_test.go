package misaligned_atomic_field

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/staticcheck/sa1027"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// platform is a target the fixtures are loaded for, whose sizes the check reads.
type platform struct {
	// architecture is the platform's `GOARCH`.
	architecture string
	// judged is whether the platform's pointers are 4 bytes, the only platforms the check judges.
	judged bool
}

// platforms are the targets the fixtures are loaded for: two 32-bit platforms, where a 64-bit word's alignment is the
// caller's to keep, and two 64-bit ones, where it is not and the check finds nothing. Each is a Linux target, the one
// system every one of these architectures builds for.
var platforms = []platform{
	{architecture: "386", judged: true},
	{architecture: "arm", judged: true},
	{architecture: "amd64", judged: false},
	{architecture: "arm64", judged: false},
}

// linuxEnvironment is the environment that loads the fixtures for the Linux target of target's architecture, with cgo
// off as it is by default for a target other than the machine's.
func linuxEnvironment(target platform) []string {
	return []string{"GOOS=linux", "GOARCH=" + target.architecture, "CGO_ENABLED=0"}
}

// staticcheck's own fixture for SA1027, copied from staticcheck/sa1027/testdata/go1.0/CheckAtomicAlignment into
// testdata/src/CheckAtomicAlignment unchanged but for the `//go:build` line gofmt writes above each file's
// `// +build` line, the same constraint, which the house's gofmt guard asks of every fixture. The fixture is two
// files, each constrained to the 32-bit or the 64-bit platforms, and staticcheck runs it on the machine's own, so the
// test runs it on each of platforms: on a 32-bit one the port meets the notes of the 32-bit file, and on a 64-bit one,
// whose file holds the same calls unnoted, it finds nothing. The go1.0 folder sets the language version of
// staticcheck's run, which no part of SA1027 reads, so the fixture loads from a tree laid out by import path as the
// house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, target := range platforms {
		test.Run(target.architecture, func(test *testing.T) {
			test.Parallel()
			if !target.judged {
				requireNothingOn(test, target, "CheckAtomicAlignment")
				return
			}
			parity.On(linuxEnvironment(target)...).RequireStaticcheckWants(test, Analyzer, analysistest.TestData(),
				"CheckAtomicAlignment")
		})
	}
}

// Over staticcheck's fixture and parity's, the second holding the shapes staticcheck's representation builds a field's
// address or a call in more than one way, the original and the port find the same findings on each of platforms, and
// on a 64-bit one both find nothing.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, target := range platforms {
		test.Run(target.architecture, func(test *testing.T) {
			test.Parallel()
			if !target.judged {
				requireNothingOn(test, target, "CheckAtomicAlignment", "parity")
				return
			}
			parity.On(linuxEnvironment(target)...).RequireIdentical(test, sa1027.Analyzer, Analyzer,
				analysistest.TestData(), "CheckAtomicAlignment", "parity")
		})
	}
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck and records no
// difference, since there is none, so on a 32-bit platform the two find the same there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.On(linuxEnvironment(platforms[0])...).RequireIdentical(test, sa1027.Analyzer, Analyzer,
		analysistest.TestData(), "differences")
}

// requireNothingOn holds that on target, a 64-bit platform, the original and the port both find nothing in the
// fixture packages patterns name, since a finding there would mean the fixtures were not checked for the platform
// named. It is the port's own check, over the harness's Findings, because every Require call fails where the original
// finds nothing, two empty sides proving nothing by themselves; the 32-bit platforms prove these fixtures hold
// findings.
func requireNothingOn(test *testing.T, target platform, patterns ...string) {
	test.Helper()
	originalFindings, portFindings := parity.On(linuxEnvironment(target)...).Findings(test, sa1027.Analyzer, Analyzer,
		analysistest.TestData(), patterns...)
	test.Logf("the original found %d, the port %d", len(originalFindings), len(portFindings))
	for _, found := range originalFindings {
		test.Errorf("the original found %s on a 64-bit platform, so the fixtures were not checked for it", found)
	}
	for _, found := range portFindings {
		test.Errorf("the port found %s on a 64-bit platform, where the check judges nothing", found)
	}
}

// In a generic function's own body, a field placed after one a type parameter sizes has no offset, and go/types
// panics when the original asks for one on a 32-bit platform, in a goroutine of the checker the test cannot recover,
// so the original does not run over the type_parameters fixture; the panic was seen in Offsetsof, at go/types'
// gcsizes.go assertion that Alignof is never asked of a type parameter. The port runs alone there, on a 32-bit
// platform, and must find exactly what the fixture records with `// port finds:`: nothing at those fields, and the
// misaligned field of a plain struct in the same body, which proves it judged the body at all. Every harness call
// runs the original beside the port, so this test is the port's own, its load and its comparison.
func TestPortJudgesNothingWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	content, readError := os.ReadFile(filepath.Join(directory, "src", "type_parameters", "type_parameters.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	recorded := []parity.Finding{}
	for index, line := range strings.Split(string(content), "\n") {
		_, record, isRecord := strings.Cut(line, "// port ")
		if !isRecord {
			continue
		}
		message, isFound := strings.CutPrefix(record, "finds: ")
		if !isFound {
			test.Fatalf("type_parameters.go:%d records %q, and the original never runs here, so it can only "+
				"record what the port finds", index+1, record)
		}
		recorded = append(recorded, parity.Finding{File: "type_parameters.go", Line: index + 1, Message: message})
	}
	if len(recorded) == 0 {
		test.Fatal("the fixture records nothing the port finds, so the test proves nothing")
	}
	// The fixture tree loads as the harness loads it, for the first of platforms.
	loaded, loadError := load.Packages(directory, []string{"type_parameters"}, append([]string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, linuxEnvironment(platforms[0])...), nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatal("loading type_parameters found no package, so the test would prove nothing")
	}
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean: %s", failure.Package, failure.Error)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{Analyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	port := []parity.Finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			port = append(port, parity.Finding{
				File:    filepath.Base(position.Filename),
				Line:    position.Line,
				Message: diagnostic.Message,
			})
		}
	}
	test.Logf("the port found %d, the fixture records %d", len(port), len(recorded))
	for _, unrecorded := range parity.Difference(port, recorded) {
		test.Errorf("the port found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range parity.Difference(recorded, port) {
		test.Errorf("the fixture records that the port finds %s, and it does not", unmatched)
	}
}
