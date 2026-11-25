package check

import (
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/correctness_require_response_status_check"
	"github.com/system-inc/cohere/internal/go_language/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/internal/go_language/rules/testing_no_uncached_run"
)

// loadPackageVariableFixture loads the one package of the module under root.
func loadPackageVariableFixture(test *testing.T, root string) *packages.Package {
	test.Helper()
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 || len(loaded.Packages) != 1 {
		test.Fatalf("loading the fixture: %v %+v, %d packages", loadError, loaded.Diagnostics, len(loaded.Packages))
	}
	return loaded.Packages[0]
}

// requireStandingFindsTheWhole runs confined over source, once with every body heard and once with the function named
// stoodName standing, its findings there the whole run's, and fails unless both find the same and the function stood
// for the rule, and is what the whole run found, each finding by its line, counted. A rule confined for the standing
// bodies may carry state across units only where none of it can come from a body it does not hear, so a package
// variable that one function writes and another reads, as source has, must be no state of its.
func requireStandingFindsTheWhole(
	test *testing.T,
	confined rule.Rule,
	source string,
	stoodName string,
) map[string]int {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, map[string]string{"go.mod": "module example.com/lazy\n\ngo 1.24\n", "lazy.go": source})
	whole := loadPackageVariableFixture(test, root)
	wholeRun, wholeError := runIsolated([]rule.Rule{confined}, []*packages.Package{whole}, nil)
	if wholeError != nil || len(wholeRun.crashed[whole]) > 0 {
		test.Fatalf("the whole run failed: %v %v", wholeError, wholeRun.crashed[whole])
	}

	// A load of its own, since a run lets go of what it analyzed.
	analyzed := loadPackageVariableFixture(test, root)
	units := functionUnits(analyzed)
	stood := slices.IndexFunc(units, func(unit functionUnit) bool { return unit.name == stoodName })
	if stood < 0 {
		test.Fatalf("the fixture declares no function %s among %v", stoodName, units)
	}
	standing := &isolationStanding{stubbed: units[stood : stood+1], whole: wholeRun.checked[whole],
		declarations: map[string]int{}}
	standingRun, standingError := runIsolatedWith([]rule.Rule{confined}, []*packages.Package{analyzed}, nil,
		func(options *checker.Options) {
			options.StandingBodies = func(string, any) rule.StandingBodies { return standing }
		})
	if standingError != nil || len(standingRun.crashed[analyzed]) > 0 {
		test.Fatalf("the standing run failed: %v %v", standingError, standingRun.crashed[analyzed])
	}
	wholeFound, standingFound := keptFindings(wholeRun.findings[whole], nil),
		keptFindings(standingRun.findings[analyzed], nil)
	if !maps.Equal(standingFound, wholeFound) {
		test.Errorf("with %s standing the rule finds %v, want what it finds hearing every body, %v", stoodName,
			standingFound, wholeFound)
	}
	stoodThere := 0
	for _, declarations := range standing.declarations {
		stoodThere += declarations
	}
	if stoodThere != 1 {
		test.Errorf("%d declarations stood for the rule, want %s alone", stoodThere, stoodName)
	}
	return wholeFound
}

// requireFindsWithALocal is the positive control of a package variable's pin: source, the pin's shape with a local of
// Kept's own in place of the package variable, run as the pin runs, must find something, so the pin's equality is no
// empty compared with empty, and the rule reads the shape the pin plants.
func requireFindsWithALocal(test *testing.T, confined rule.Rule, source string) {
	test.Helper()
	if found := requireStandingFindsTheWhole(test, confined, source, "Stood"); len(found) == 0 {
		test.Errorf("with a local in place of the package variable %s finds nothing, want its finding in Kept",
			confined.Name)
	}
}

// security-no-interpolated-sql-string reads a local assigned once in place, and never a package variable
// (#e4rd5dw): prefix, declared with a quote left open and written again in Stood, which stands, would read as its
// initializer were it followed, and Kept's name would then sit inside a quoted string, a finding the build makes only
// because it never heard Stood's write. With a prefix of Kept's own the rule finds name, so the pin compares what the
// rule does find.
func TestTheInterpolatedSqlRuleReadsNoPackageVariableAcrossAStandingBody(test *testing.T) {
	test.Parallel()
	requireStandingFindsTheWhole(test, security_no_interpolated_sql_string.Rule, "package lazy\n\n"+
		"import \"database/sql\"\n\n"+
		"var prefix = \"SELECT id FROM users WHERE name = '\"\n\n"+
		"// Stood points the prefix at the id.\nfunc Stood() {\n\tprefix = \"SELECT id FROM users WHERE id = \"\n}\n\n"+
		"// Kept queries by name.\nfunc Kept(database *sql.DB, name string) {\n"+
		"\tdatabase.Query(prefix + name + \"'\")\n}\n", "Stood")
	requireFindsWithALocal(test, security_no_interpolated_sql_string.Rule, "package lazy\n\n"+
		"import \"database/sql\"\n\n"+
		"// Stood points its own prefix at the id.\nfunc Stood() string {\n"+
		"\tprefix := \"SELECT id FROM users WHERE id = \"\n\treturn prefix\n}\n\n"+
		"// Kept queries by name.\nfunc Kept(database *sql.DB, name string) {\n"+
		"\tprefix := \"SELECT id FROM users WHERE name = '\"\n\tdatabase.Query(prefix + name + \"'\")\n}\n")
}

// testing-no-uncached-run reads a slice spread into a go command from a local, and never from a package variable
// (#e4rd5dw): arguments, declared with -count=1 and written again in Stood, which stands, would read as its literal
// were it followed, a finding at the package's own declaration the build makes only because it never heard Stood's
// write. With arguments of Kept's own the rule finds -count=1, so the pin compares what the rule does find.
func TestTheUncachedRunRuleReadsNoPackageVariableAcrossAStandingBody(test *testing.T) {
	test.Parallel()
	requireStandingFindsTheWhole(test, testing_no_uncached_run.Rule, "package lazy\n\n"+
		"import \"os/exec\"\n\n"+
		"var arguments = []string{\"test\", \"-count=1\", \"./...\"}\n\n"+
		"// Stood keeps the cache.\nfunc Stood() {\n\targuments = []string{\"test\", \"./...\"}\n}\n\n"+
		"// Kept runs the tests.\nfunc Kept() *exec.Cmd {\n\treturn exec.Command(\"go\", arguments...)\n}\n", "Stood")
	requireFindsWithALocal(test, testing_no_uncached_run.Rule, "package lazy\n\n"+
		"import \"os/exec\"\n\n"+
		"// Stood keeps its own cache.\nfunc Stood() []string {\n"+
		"\targuments := []string{\"test\", \"./...\"}\n\treturn arguments\n}\n\n"+
		"// Kept runs the tests.\nfunc Kept() *exec.Cmd {\n"+
		"\targuments := []string{\"test\", \"-count=1\", \"./...\"}\n"+
		"\treturn exec.Command(\"go\", arguments...)\n}\n")
}

// correctness-require-response-status-check tracks a response declared in a function, and never a package variable
// (#e4rd5dw): response, fetched and its body read in Kept, has its status read in Stood, which stands, so were it
// tracked Kept's read would be a finding the build makes only because it never heard Stood's check. With a response
// of Kept's own the rule finds that read, so the pin compares what the rule does find.
func TestTheResponseStatusRuleTracksNoPackageVariableAcrossAStandingBody(test *testing.T) {
	test.Parallel()
	requireStandingFindsTheWhole(test, correctness_require_response_status_check.Rule, "package lazy\n\n"+
		"import (\n\t\"io\"\n\t\"net/http\"\n)\n\n"+
		"var response *http.Response\n\n"+
		"// Stood reads the status.\nfunc Stood() int {\n\treturn response.StatusCode\n}\n\n"+
		"// Kept fetches and reads the body.\nfunc Kept() ([]byte, error) {\n\tvar fetchError error\n"+
		"\tresponse, fetchError = http.Get(\"http://example.com\")\n\tif fetchError != nil {\n"+
		"\t\treturn nil, fetchError\n\t}\n\treturn io.ReadAll(response.Body)\n}\n", "Stood")
	requireFindsWithALocal(test, correctness_require_response_status_check.Rule, "package lazy\n\n"+
		"import (\n\t\"io\"\n\t\"net/http\"\n)\n\n"+
		"// Stood reads the status of its own response.\nfunc Stood(response *http.Response) int {\n"+
		"\treturn response.StatusCode\n}\n\n"+
		"// Kept fetches and reads the body.\nfunc Kept() ([]byte, error) {\n"+
		"\tresponse, fetchError := http.Get(\"http://example.com\")\n\tif fetchError != nil {\n"+
		"\t\treturn nil, fetchError\n\t}\n\treturn io.ReadAll(response.Body)\n}\n")
}
