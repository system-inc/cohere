package unused_result

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/unusedresult"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// upstreamFunctions is the funcs list x/tools' test sets before it runs its fixtures, which adds the generic
// userdefs.MustUse that typeparams' wants expect.
const upstreamFunctions = "typeparams/userdefs.MustUse,errors.New,fmt.Errorf,fmt.Sprintf,fmt.Sprint"

// x/tools' own fixtures for unusedresult, copied unchanged into testdata/src and run under x/tools' test's funcs
// list, hold the port to every want the original expects, and to nothing more. The flag is set on a copy of the
// analyzer, so the registry's keeps its default, and the copy sits on a syntax dispatcher of its own, since a
// dispatcher names each rule once.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	configured := newAnalyzer(dispatch.NewSyntax())
	if setError := configured.Flags.Set(functionsOption, upstreamFunctions); setError != nil {
		test.Fatal(setError)
	}
	parity.RequireUpstreamWants(test, configured, analysistest.TestData(), "a", "typeparams")
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
// differ, and their findings must be the same multiset, each the same in every field. They run under the
// default flags, x/tools' test's, lists naming parity's own functions and methods with every malformed entry the
// parser skips, and empty lists, since the flags decide what is judged. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails where the original finds nothing, except under the empty
// lists, where it fails if either side finds anything.
//
// The original binds its flags to package variables, so its settings are set here one configuration at a time and put
// back to their defaults at the end. No other test in this package touches the original, so the test still runs in
// parallel with them. The harness has no form for a run under flags, and RequireIdentical fails where the original
// finds nothing, which the empty lists require, so the loop is this test's own, and each configuration's findings come
// from the harness's Findings, with its gates, and are compared whole by its Difference.
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
		original, ported := parity.Findings(test, unusedresult.Analyzer, port, analysistest.TestData(), "a",
			"typeparams", "parity")
		test.Logf("under %s, the original found %d, the port %d", configured.name, len(original), len(ported))
		switch {
		case configured.findsNothing && len(original)+len(ported) > 0:
			test.Errorf("under %s, the lists name nothing, and something was found", configured.name)
		case !configured.findsNothing && len(original) == 0:
			test.Errorf("under %s, the original found nothing in the fixtures, so the comparison proves nothing",
				configured.name)
		}
		for _, missing := range parity.Difference(original, ported) {
			test.Errorf("under %s, the original found %s, and the port did not", configured.name, missing)
		}
		for _, extra := range parity.Difference(ported, original) {
			test.Errorf("under %s, the port found %s, and the original did not", configured.name, extra)
		}
	}
}
