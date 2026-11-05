package correctness_require_declared_walk_test

import (
	"flag"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/correctness_require_declared_walk"
	"github.com/system-inc/cohere/policy"
)

// readsFile is the reads file the configured runs name, the one Adamic's rule hardcoded, which the flagged fixture's
// first want spells out in full.
const readsFile = "cloud/fast-gate/executors.txt"

// configured is a copy of the analyzer with readsFile set, so the configured run and the unconfigured original can run
// in parallel without one setting a flag the other reads. The copy gets flags of its own, declared afresh from the
// original's, since a copied flag set shares the original's flag values.
func configured(test *testing.T) *analysis.Analyzer {
	test.Helper()
	analyzer := *correctness_require_declared_walk.Analyzer
	analyzer.Flags = flag.FlagSet{}
	correctness_require_declared_walk.Analyzer.Flags.VisitAll(func(option *flag.Flag) {
		analyzer.Flags.String(option.Name, option.DefValue, option.Usage)
	})
	if setError := analyzer.Flags.Set("readsFile", readsFile); setError != nil {
		test.Fatal(setError)
	}
	return &analyzer
}

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := correctness_require_declared_walk.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "correctness-require-declared-walk")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every source the rule must report, a marker with no glob under its own
// message, and analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and
// lookalike fixtures prove the rule stays quiet on a walk inside the package, an untraceable root, a declared walk,
// one declared beside an empty marker, a fixed file, and a walk that isn't the standard library's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), configured(test), "flagged", "allowed", "lookalike")
}

// Unconfigured, the rule reports nothing, even on the flagged fixture: a repository with no planner reading reads
// lines has nothing a declaration would feed. The flagged fixture's wants go unmatched by design, so analysistest's
// own complaints are set aside and the findings are counted directly.
func TestUnconfiguredReportsNothing(test *testing.T) {
	test.Parallel()
	patterns := []string{"flagged", "allowed", "lookalike"}
	results := analysistest.Run(
		discarded{},
		analysistest.TestData(),
		correctness_require_declared_walk.Analyzer,
		patterns...,
	)
	if len(results) != len(patterns) {
		test.Fatalf("analyzed %d packages, expected %d, so a fixture failed to load", len(results), len(patterns))
	}
	for _, result := range results {
		if result.Action.Err != nil {
			test.Errorf("analyzing %s: %v", result.Action.Package.PkgPath, result.Action.Err)
		}
		for _, diagnostic := range result.Action.Diagnostics {
			test.Errorf("%s reported %q with readsFile unset", result.Action.Package.PkgPath, diagnostic.Message)
		}
	}
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}

// discarded is a reporter that drops what analysistest says, for the run whose wants go unmatched on purpose.
type discarded struct{}

func (discarded) Errorf(format string, arguments ...any) {}
