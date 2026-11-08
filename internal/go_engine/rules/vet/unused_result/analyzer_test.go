package unused_result

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/unusedresult"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// upstreamFunctions is the funcs list x/tools' test sets before it runs its fixtures, which adds the generic
// userdefs.MustUse that typeparams' wants expect.
const upstreamFunctions = "typeparams/userdefs.MustUse,errors.New,fmt.Errorf,fmt.Sprintf,fmt.Sprint"

// x/tools' own fixtures for unusedresult, copied unchanged into testdata/src and run as x/tools' test runs them, with
// its funcs list: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either. The flag is set on a copy of the analyzer, so the registry's keeps its default, and the copy sits on a
// syntax dispatcher of its own, since a dispatcher names each rule once.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	configured := newAnalyzer(dispatch.NewSyntax())
	if setError := configured.Flags.Set(functionsOption, upstreamFunctions); setError != nil {
		test.Fatal(setError)
	}
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), configured, "a", "typeparams")
}

// configuration is one setting of the two flags the differential runs both sides under. A flag flags leaves out
// keeps its default.
type configuration struct {
	name  string
	flags map[string]string
	// findsNothing is true for the lists that name nothing, where both sides must find nothing.
	findsNothing bool
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every function and method of the default lists, every branch of the judgment and the shapes most likely to
// differ, and their findings must be the same multiset of file, line, column, end and message. They run under the
// default flags, x/tools' test's, lists naming parity's own functions and methods with every malformed entry the
// parser skips, and empty lists, since the flags decide what is judged. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails where the original finds nothing, except under the empty
// lists, where it fails if either side finds anything.
//
// The original binds its flags to package variables, so its settings are set here one configuration at a time and put
// back to their defaults at the end. No other test in this package touches the original, so the test still runs in
// parallel with them.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	configurations := []configuration{
		{name: "the default flags", flags: map[string]string{}},
		{name: "x/tools' test's flags", flags: map[string]string{functionsOption: upstreamFunctions}},
		{
			name: "parity's own lists",
			flags: map[string]string{
				// A name with no dot, one with only a leading dot, one with a trailing dot and empty entries are
				// skipped or match nothing; a method named as a function matches nothing; errors.New stays judged.
				functionsOption: "parity.Format,parity.Identity,,parity.Both,fmt,.Sprint,fmt.,parity.shapes.Name," +
					"errors.New,",
				stringMethodsOption: "Name,GoString,Count,,",
			},
		},
		{
			name:         "empty lists",
			flags:        map[string]string{functionsOption: "", stringMethodsOption: ""},
			findsNothing: true,
		},
	}
	defer func() {
		for _, option := range []string{functionsOption, stringMethodsOption} {
			restored := unusedresult.Analyzer.Flags.Lookup(option).DefValue
			if setError := unusedresult.Analyzer.Flags.Set(option, restored); setError != nil {
				test.Errorf("putting the original's %s back: %v", option, setError)
			}
		}
	}()
	for _, configured := range configurations {
		// A copy on a dispatcher of its own for each configuration, for the reason the wants test gives.
		port := newAnalyzer(dispatch.NewSyntax())
		for _, option := range []string{functionsOption, stringMethodsOption} {
			value, set := configured.flags[option]
			if !set {
				value = unusedresult.Analyzer.Flags.Lookup(option).DefValue
			}
			if setError := unusedresult.Analyzer.Flags.Set(option, value); setError != nil {
				test.Fatalf("setting the original's %s: %v", option, setError)
			}
			if setError := port.Flags.Set(option, value); setError != nil {
				test.Fatalf("setting the port's %s: %v", option, setError)
			}
		}
		original, ported := findingsOf(test, port, "a", "typeparams", "parity")
		test.Logf("under %s, the original found %d, the port %d", configured.name, len(original), len(ported))
		switch {
		case configured.findsNothing && len(original)+len(ported) > 0:
			test.Errorf("under %s, the lists name nothing, and something was found", configured.name)
		case !configured.findsNothing && len(original) == 0:
			test.Errorf("under %s, the original found nothing in the fixtures, so the comparison proves nothing",
				configured.name)
		}
		for _, missing := range difference(original, ported) {
			test.Errorf("under %s, the original found %s, and the port did not", configured.name, described(missing))
		}
		for _, extra := range difference(ported, original) {
			test.Errorf("under %s, the port found %s, and the original did not", configured.name, described(extra))
		}
	}
}

// finding is one finding as the differential compares it, with where it starts and where it ends.
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

// findingsOf loads the fixture packages patterns name, runs x/tools' unusedresult and port over them in one checker,
// and returns each one's findings, sorted.
func findingsOf(test *testing.T, port *analysis.Analyzer, patterns ...string) (original []finding, ported []finding) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// The checker passes over a root that does not type-check without a word, so a fixture with one type error would
	// compare nothing and read green. Every root must load clean, and there must be one.
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{unusedresult.Analyzer, port}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
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
			case unusedresult.Analyzer:
				original = append(original, found)
			case port:
				ported = append(ported, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(ported, compare)
	return original, ported
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

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
