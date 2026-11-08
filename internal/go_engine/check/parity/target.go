package parity

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// Target is the build target a port's fixtures load for, named by the environment variables that choose it, as
// `GOOS`, `GOARCH` and `CGO_ENABLED` do. A check that judges one platform, as SA1027 judges only a 32-bit one, finds
// nothing on another, so its fixtures load for the target it judges. Its methods are the package's Require functions
// and Findings, each the same but for the load, and the package's own load for the machine's own target, the zero
// Target's. RequireUpstreamWants has no method: analysistest loads its fixtures under an environment of its own.
type Target struct {
	// environment is the variables the caller named, each `NAME=value`, appended after the fixture load's own.
	environment []string
}

// On is the target environment names, each variable written `NAME=value`, as `On("GOOS=linux", "GOARCH=386",
// "CGO_ENABLED=0")` names a 32-bit Linux one, with cgo off as it is by default for a target other than the machine's.
func On(environment ...string) Target {
	return Target{environment: slices.Clone(environment)}
}

// Findings is the package's Findings with the fixtures loaded for target.
func (target Target) Findings(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) (originalFindings []Finding, portFindings []Finding) {
	test.Helper()
	return findingsOver(test, original, port, loadFixtures(test, target, testdata, patterns, nil))
}

// RequireIdentical is the package's RequireIdentical with the fixtures loaded for target.
func (target Target) RequireIdentical(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireIdentical(test, target, original, port, testdata, nil, patterns...)
}

// RequireOnlyAdjudicated is the package's RequireOnlyAdjudicated with the fixtures loaded for target.
func (target Target) RequireOnlyAdjudicated(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
) {
	test.Helper()
	requireOnlyAdjudicated(test, target, original, port, testdata, fixture)
}

// RequireStaticcheckWants is the package's RequireStaticcheckWants with the fixtures loaded for target.
func (target Target) RequireStaticcheckWants(
	test testing.TB,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireStaticcheckWants(test, target, port, testdata, patterns...)
}

// RequireStaticcheckGolden is the package's RequireStaticcheckGolden with the fixtures loaded for target.
func (target Target) RequireStaticcheckGolden(
	test testing.TB,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireStaticcheckGolden(test, target, port, testdata, patterns...)
}

// RequirePortWhereOriginalPanics is the package's RequirePortWhereOriginalPanics with the fixture loaded for target.
func (target Target) RequirePortWhereOriginalPanics(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
) {
	test.Helper()
	requirePortWhereOriginalPanics(test, target, original, port, testdata, fixture)
}

// environmentOf is the environment fixtures load under for target: the fixture load's own, then target's. A variable
// target names that has no value, or that the fixture load already sets, fails the test, since the second would
// override how the fixtures are found rather than the target they are checked for.
func (target Target) environmentOf(test testing.TB, fixtures fixtureLoad) []string {
	test.Helper()
	own := fixtureEnvironment(fixtures.directory)
	for _, variable := range target.environment {
		name, _, hasValue := strings.Cut(variable, "=")
		if !hasValue {
			test.Fatalf("the target's %q names no value: write it NAME=value", variable)
		}
		if slices.ContainsFunc(own, func(set string) bool { return strings.HasPrefix(set, name+"=") }) {
			test.Fatalf("the target sets %s, which the load of %s sets itself", name, fixtures.described())
		}
	}
	return slices.Concat(own, target.environment)
}
