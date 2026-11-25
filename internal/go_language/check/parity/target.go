package parity

import (
	"go/build"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// Target is the build target a port's fixtures load for, named by the environment variables that choose it, as
// `GOOS`, `GOARCH` and `CGO_ENABLED` do. A check that judges one platform, as SA1027 judges only a 32-bit one, finds
// nothing on another, so its fixtures load for the target it judges. Its methods are the package's Require functions
// and Findings, each the same but for the load, and the package's own load for the machine's own target, the zero
// Target's. RequireUpstreamWants has no method: analysistest loads its fixtures under an environment of its own.
//
// A check can read its target from go/build's default context, build.Default, as asmdecl and framepointer, original
// and port alike, read the architecture and the system they judge, and go/build fixes that from the environment as
// the process begins, before any test can name one. So a Target that names `GOOS` or `GOARCH` sets that field of
// build.Default as well, for its call's load and run, and puts it back when the call ends, failed or not (see
// contextFields). build.Default is the process's own and tests run in parallel, so every call of the harness holds it
// (see held): a call on such a Target holds it alone, and every other call holds it beside the rest, so no call ever
// reads a build.Default another call has set.
//
// A test outside the harness holds none of this. One that reads build.Default by hand reads whatever a harness call
// beside it has set, so it runs in parallel with no test of its package that calls the harness on a Target naming
// `GOOS` or `GOARCH`, and one that sets it runs in parallel with no harness call at all. RequireUpstreamWants, which
// takes no Target, runs under the default context as it stands, so a port whose upstream fixtures need another target
// there still sets it by hand, under those rules.
type Target struct {
	// environment is the variables the caller named, each `NAME=value`, appended after the fixture load's own.
	environment []string

	// judgedBy is the `GOOS` and `GOARCH` the caller named for build.Default alone, each `NAME=value`, over the
	// environment's (see JudgingBy).
	judgedBy []string
}

// On is the target environment names, each variable written `NAME=value`, as `On("GOOS=linux", "GOARCH=386",
// "CGO_ENABLED=0")` names a 32-bit Linux one, with cgo off as it is by default for a target other than the machine's.
// The `GOOS` and `GOARCH` it names are set in build.Default for each call's load and run, and put back in a deferred
// release, so a call that fails never leaves them behind. build.Default stays a global to everything outside the
// harness's calls: test code in a port's package that reads it outside one races a call that sets it, so it runs in
// parallel with none of them (see Target).
func On(environment ...string) Target {
	return Target{environment: slices.Clone(environment)}
}

// JudgingBy is target with the `GOOS` and `GOARCH` variables names set in build.Default alone, over any target names
// for the load, each written `NAME=value`, so the load hands the check files of one target while it judges as on
// another. A test wants it for a file the load hands over whose architecture the check must not be able to name, as
// `On("GOOS=linux", "GOARCH=amd64").JudgingBy("GOARCH=sparc64")` hands asmdecl parity_go_build.s, built for amd64 by
// a `//go:build` line the pass does not read, while the default context names an architecture it does not know, so
// the pass can tell none for the file. go list loads no file for a target it cannot build, so only this hands such a
// file over. Its call holds the default context to write and puts it back as one on a target setting it by On does,
// and is refused what On is, a variable with no value, named twice among these, or empty, and any but `GOOS` and
// `GOARCH`.
func (target Target) JudgingBy(variables ...string) Target {
	target.judgedBy = slices.Concat(target.judgedBy, variables)
	return target
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
	defer target.held(test)()
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

// RequireIdenticalFacts is the package's RequireIdenticalFacts with the fixtures loaded for target.
func (target Target) RequireIdenticalFacts(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireIdenticalFacts(test, target, original, port, testdata, patterns...)
}

// RequireIdenticalLeavingAlone is the package's RequireIdenticalLeavingAlone with the fixtures loaded for target.
func (target Target) RequireIdenticalLeavingAlone(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	alone []string,
	patterns ...string,
) {
	test.Helper()
	requireIdenticalLeavingAlone(test, target, original, port, testdata, alone, patterns...)
}

// RequireNeitherFinds is the package's RequireNeitherFinds with the fixtures loaded for target.
func (target Target) RequireNeitherFinds(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	judged func(test testing.TB, loaded []*packages.Package) int,
	patterns ...string,
) {
	test.Helper()
	requireNeitherFinds(test, target, original, port, testdata, judged, patterns...)
}

// RequireOnlyAdjudicated is the package's RequireOnlyAdjudicated with the fixtures loaded for target.
func (target Target) RequireOnlyAdjudicated(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	given ...Record,
) {
	test.Helper()
	requireOnlyAdjudicated(test, target, original, port, testdata, fixture, "", given)
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
	given ...Record,
) {
	test.Helper()
	requirePortWhereOriginalPanics(test, target, original, port, testdata, fixture, given)
}

// contextFields are the variables a Target sets in go/build's default context, build.Default, beside the
// environment, each with the field of a context it sets: the system and the architecture a check reads its target
// from there. `CGO_ENABLED` is not among them: no original or port this repository holds reads the default context's
// CgoEnabled, and the load sets its own from the environment.
var contextFields = map[string]func(context *build.Context) *string{
	"GOOS":   func(context *build.Context) *string { return &context.GOOS },
	"GOARCH": func(context *build.Context) *string { return &context.GOARCH },
}

// defaultContext guards build.Default for the harness's calls: a call on a Target that sets it holds defaultContext
// to write for its whole load and run, and every other call holds it to read (see held).
var defaultContext sync.RWMutex

// held is target held for one call's load and run, and what lets it go, which the call defers, so a call that fails
// lets go too. A target that names `GOOS` or `GOARCH`, by On or by JudgingBy, whose value wins, sets each in
// build.Default and holds defaultContext to write, and letting go puts back what it set; any other target holds it to
// read, since the load copies build.Default and a check may read it. So a call reads build.Default only as its own
// target set it or as the process began, and no call sets it while another reads it. A variable named with no value,
// a variable named twice among On's or among JudgingBy's (see namedOnce), an empty `GOOS` or `GOARCH`, a target
// build.Default cannot judge by, and a JudgingBy variable other than those two each fail the test before anything is
// held. sync.RWMutex is not reentrant, so a call holds it once, at its start, and nothing
// it runs while holding it calls the harness again: a hold taken beneath one to write waits forever, and one beneath
// one to read waits forever behind a call waiting to write. So every Require function and Findings holds it in its
// own require or findings function, which no other holds beneath it, findingsOver and loadFixtures hold nothing,
// Analyze holds it and analyze, what the harness runs within a hold, does not, and RequireNeitherFinds's judged runs
// after its hold lets go.
func (target Target) held(test testing.TB) (release func()) {
	test.Helper()
	judged := map[string]string{}
	for _, variable := range namedOnce(test, target.environment) {
		if _, isContextField := contextFields[variable.name]; isContextField {
			judged[variable.name] = nonEmpty(test, variable)
		}
	}
	for _, variable := range namedOnce(test, target.judgedBy) {
		if _, isContextField := contextFields[variable.name]; !isContextField {
			test.Fatalf("the target judges by %s, and go/build's default context holds only GOOS and GOARCH for a "+
				"check to read", variable.name)
		}
		judged[variable.name] = nonEmpty(test, variable)
	}
	if len(judged) == 0 {
		defaultContext.RLock()
		return defaultContext.RUnlock
	}
	defaultContext.Lock()
	saved := map[string]string{}
	for name, value := range judged {
		field := contextFields[name](&build.Default)
		saved[name], *field = *field, value
	}
	return func() {
		for name, value := range saved {
			*contextFields[name](&build.Default) = value
		}
		defaultContext.Unlock()
	}
}

// namedVariable is one variable a target names, its name, its value and how it was written.
type namedVariable struct {
	name    string
	value   string
	written string
}

// namedOnce is variables read, each written `NAME=value`, in order. One with no value fails the test, and so does one
// named twice, since which value counts would be the order's accident.
func namedOnce(test testing.TB, variables []string) []namedVariable {
	test.Helper()
	read := []namedVariable{}
	for _, written := range variables {
		name, value, hasValue := strings.Cut(written, "=")
		if !hasValue {
			test.Fatalf("the target's %q names no value: write it NAME=value", written)
		}
		isEarlier := func(earlier namedVariable) bool { return earlier.name == name }
		if index := slices.IndexFunc(read, isEarlier); index >= 0 {
			test.Fatalf("the target names %s twice, as %q and %q, so which it is for is the order's accident: name "+
				"it once", name, read[index].written, written)
		}
		read = append(read, namedVariable{name: name, value: value, written: written})
	}
	return read
}

// nonEmpty is variable's value, for a variable build.Default takes, failing the test where it is empty.
func nonEmpty(test testing.TB, variable namedVariable) string {
	test.Helper()
	if variable.value == "" {
		test.Fatalf("the target's %s is empty, and go/build's default context judges by no empty target",
			variable.name)
	}
	return variable.value
}

// environmentOf is the environment fixtures load under for target: the fixture load's own, then target's. A variable
// target names that the fixture load already sets fails the test, since it would override how the fixtures are found
// rather than the target they are checked for. held has already refused one with no value.
func (target Target) environmentOf(test testing.TB, fixtures fixtureLoad) []string {
	test.Helper()
	own := fixtureEnvironment(fixtures.directory)
	for _, variable := range target.environment {
		name, _, _ := strings.Cut(variable, "=")
		if slices.ContainsFunc(own, func(set string) bool { return strings.HasPrefix(set, name+"=") }) {
			test.Fatalf("the target sets %s, which the load of %s sets itself", name, fixtures.described())
		}
	}
	return slices.Concat(own, target.environment)
}
