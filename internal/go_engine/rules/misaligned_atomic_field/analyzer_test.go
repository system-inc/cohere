package misaligned_atomic_field

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1027"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
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
// panics when the original asks for one on a 32-bit platform; the panic was seen in Offsetsof, at go/types'
// gcsizes.go assertion that Alignof is never asked of a type parameter. So over the type_parameters fixture, on a
// 32-bit platform, the original must panic, and the port must find exactly what the fixture records with
// `// port finds:`: nothing at those fields, and the misaligned field of a plain struct in the same body, which proves
// it judged the body at all.
func TestPortJudgesNothingWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.On(linuxEnvironment(platforms[0])...).RequirePortWhereOriginalPanics(test, sa1027.Analyzer, Analyzer,
		analysistest.TestData(), "type_parameters")
}
