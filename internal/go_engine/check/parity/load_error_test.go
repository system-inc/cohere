package parity

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// despiteListed are the errors the despite fixture loads with, each once, though its test variant reports the first
// two again.
var despiteListed = []LoadError{
	{
		Position: "despite.go:12:17",
		Message:  `cannot use "many" (untyped string constant) as int value in variable declaration`,
	},
	{
		Position: "despite.go:14:17",
		Message:  `cannot use "many" (untyped string constant) as int value in variable declaration`,
	},
	{
		Position: "despite_test.go:3:19",
		Message:  "cannot use 1 (untyped int constant) as bool value in variable declaration",
	},
}

// originalDespiteErrors and portDespiteErrors are the pair that agrees on despite, both running despite errors, two
// analyzers since the checker refuses one given twice.
var (
	originalDespiteErrors = despiteErrors(calling(sharedMessages))
	portDespiteErrors     = despiteErrors(calling(sharedMessages))
)

func TestRequireIdenticalDespiteLoadErrorsPassesTheListedErrors(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors, analysistest.TestData(),
			despiteListed, "despite")
	}))
}

// An error the fixture loads with and the list leaves out fails, named by position and message, and nothing is
// compared after it.
func TestRequireIdenticalDespiteLoadErrorsFailsAnUnlistedError(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors, analysistest.TestData(),
			despiteListed[:2], "despite")
	}),
		"the fixture packages load with despite_test.go:3:19: cannot use 1 (untyped int constant) as bool value in "+
			"variable declaration, which is not listed",
		"the fixture packages do not fail as listed",
	)
}

// A listed error the fixture does not load with fails, and so does one listed twice that loads once, since the list
// is a multiset.
func TestRequireIdenticalDespiteLoadErrorsFailsAListedErrorThatDoesNotLoad(test *testing.T) {
	test.Parallel()
	absent := LoadError{Position: "despite.go:8:2", Message: "undefined: shared"}
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors, analysistest.TestData(),
			slices.Concat(despiteListed, []LoadError{absent, despiteListed[0]}), "despite")
	}),
		"the list holds despite.go:8:2: undefined: shared, and the fixture packages do not load with it",
		`the list holds despite.go:12:17: cannot use "many" (untyped string constant) as int value in variable `+
			"declaration, and the fixture packages do not load with it",
		"the fixture packages do not fail as listed",
	)
}

// A side that does not run despite errors is refused, each named, since the checker would skip it on the fixture.
func TestRequireIdenticalDespiteLoadErrorsRefusesAnAnalyzerThatDoesNotRunDespiteErrors(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, calling(sharedMessages), portDespiteErrors,
			analysistest.TestData(), despiteListed, "despite")
	}), "the original, calling, does not run despite errors")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, calling(sharedMessages),
			analysistest.TestData(), despiteListed, "despite")
	}), "the port, calling, does not run despite errors")
}

// An empty list is RequireIdentical, and is refused.
func TestRequireIdenticalDespiteLoadErrorsRefusesAnEmptyList(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors, analysistest.TestData(),
			nil, "same")
	}), "no load error is listed, and fixtures that load clean are RequireIdentical's")
}

// Past the errors, the findings are compared as RequireIdentical compares them: each call the port misses fails, in
// the package and again in its test variant, and an original that finds nothing fails too.
func TestRequireIdenticalDespiteLoadErrorsComparesTheFindings(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, despiteErrors(calling(nil)),
			analysistest.TestData(), despiteListed, "despite")
	}),
		"the original found despite.go:8:2: call to shared, and the port did not",
		"the original found despite.go:8:2: call to shared, and the port did not",
		"the original found despite.go:9:2: call to shared, and the port did not",
		"the original found despite.go:9:2: call to shared, and the port did not",
	)
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, despiteErrors(calling(nil)), portDespiteErrors,
			analysistest.TestData(), despiteListed, "despite")
	}), "the original found nothing")
}

// A side that runs despite errors but requires an analyzer that does not fails on the ill-typed package, since the
// checker skips the prerequisite there and with it the side.
func TestRequireIdenticalDespiteLoadErrorsFailsAnAnalyzerThatDidNotRun(test *testing.T) {
	test.Parallel()
	requiring := despiteErrors(calling(sharedMessages))
	requiring.Requires = []*analysis.Analyzer{{
		Name: "prerequisite",
		Doc:  "prerequisite does nothing, and does not run despite errors.",
		Run:  func(*analysis.Pass) (any, error) { return nil, nil },
	}}
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, requiring, analysistest.TestData(),
			despiteListed, "despite")
	}), "running on despite: failed prerequisites: prerequisite@despite")
}

// despiteAgainListed is the one error despite_again loads with, and truncatedListed truncated's, two parse errors at
// the end of a file that ends inside a function.
var (
	despiteAgainListed = LoadError{
		Position: "despite_again.go:11:17",
		Message:  "cannot use 1 (untyped int constant) as bool value in variable declaration",
	}
	truncatedListed = []LoadError{
		{Position: "truncated.go:7:11", Message: "expected ';', found 'EOF'"},
		{Position: "truncated.go:7:11", Message: "expected '}', found 'EOF'"},
	}
)

// Several ill-typed packages compare in one call, each with its own errors listed, and a package that does not parse
// is listed by its parse error as one that does not type-check is by its type error. Each package's error left off
// the list fails, named, so every package's errors are held, not only the first's.
func TestRequireIdenticalDespiteLoadErrorsHoldsEachPackagesErrors(test *testing.T) {
	test.Parallel()
	listed := slices.Concat(despiteListed, []LoadError{despiteAgainListed}, truncatedListed)
	patterns := []string{"despite", "despite_again", "truncated"}
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors, analysistest.TestData(),
			listed, patterns...)
	}))
	for _, leftOff := range []LoadError{despiteAgainListed, truncatedListed[0]} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			RequireIdenticalDespiteLoadErrors(recording, originalDespiteErrors, portDespiteErrors,
				analysistest.TestData(), slices.DeleteFunc(slices.Clone(listed), func(loadError LoadError) bool {
					return loadError == leftOff
				}), patterns...)
		}), "the fixture packages load with "+leftOff.String()+", which is not listed",
			"the fixture packages do not fail as listed")
	}
}
