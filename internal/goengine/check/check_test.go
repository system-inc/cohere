package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/system-inc/cohere/internal/goengine/load"
	"github.com/system-inc/cohere/internal/goengine/rule"
)

// targetLine is the line of base.go that declares Target. The comment before it holds an é, two bytes in UTF-8,
// so a column counted in runes rather than bytes would be one short.
const targetLine = "/* é */ func Target() int { return 2 }"

// fixtureFiles are the module the tests load: base declares Marked, which markedAnalyzer exports a fact for, and
// Target, which targetAnalyzer reports, and user imports base and calls Marked, so the fact reaches user only
// across the package boundary.
var fixtureFiles = map[string]string{
	"go.mod": "module example.com/fixture\n\ngo 1.21\n",
	"base/base.go": "package base\n\n// Marked is the function markedAnalyzer exports a fact for.\n" +
		"func Marked() int { return 1 }\n\n" + targetLine + "\n",
	"user/user.go": "package user\n\nimport \"example.com/fixture/base\"\n\n" +
		"// Use calls Marked from outside the package that marked it.\nfunc Use() int { return base.Marked() }\n",
}

// fixtures is the module written and loaded once per test binary, which every test only reads.
var fixtures struct {
	once    sync.Once
	root    string
	loaded  *load.Loaded
	failure error
}

// TestMain removes the fixtures once every test is done with them.
func TestMain(tests *testing.M) {
	code := tests.Run()
	if fixtures.root != "" {
		// Ignored: a temporary directory left behind fails nothing.
		_ = os.RemoveAll(fixtures.root)
	}
	os.Exit(code)
}

// loadedFixtures writes and loads the fixtures on first use and returns the directory they were written to.
func loadedFixtures(test *testing.T) string {
	test.Helper()

	fixtures.once.Do(func() {
		fixtures.root, fixtures.failure = os.MkdirTemp("", "cohere-goengine-check-")
		if fixtures.failure != nil {
			return
		}
		for name, contents := range fixtureFiles {
			path := filepath.Join(fixtures.root, filepath.FromSlash(name))
			if fixtures.failure = os.MkdirAll(filepath.Dir(path), 0o755); fixtures.failure != nil {
				return
			}
			if fixtures.failure = os.WriteFile(path, []byte(contents), 0o644); fixtures.failure != nil {
				return
			}
		}
		fixtures.loaded, fixtures.failure = load.Packages(fixtures.root, []string{"./..."}, []string{"GOWORK=off"}, nil)
		if fixtures.failure == nil && len(fixtures.loaded.Diagnostics) > 0 {
			fixtures.failure = fmt.Errorf("the fixtures loaded with diagnostics: %+v", fixtures.loaded.Diagnostics)
		}
	})
	if fixtures.failure != nil {
		test.Fatalf("loading the fixtures: %v", fixtures.failure)
	}
	return fixtures.root
}

// runRules runs rules over the fixtures and fails the test if the run could not start.
func runRules(test *testing.T, options Options, rules ...rule.Rule) *Result {
	test.Helper()

	loadedFixtures(test)
	result, runError := Run(fixtures.loaded.Packages, rules, options)
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	return result
}

// houseRule is analyzer as a house rule named for it.
func houseRule(analyzer *analysis.Analyzer) rule.Rule {
	return rule.Rule{Name: rule.Namespace + "test-" + analyzer.Name, Origin: rule.OriginHouse, Analyzer: analyzer}
}

// targetAnalyzer reports the name of every function named Target, with a fix that renames it, and finds the
// functions through inspect.Analyzer's result, so it fails if that result is not where ResultOf keys it.
var targetAnalyzer = &analysis.Analyzer{
	Name:     "target",
	Doc:      "target reports every function named Target",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		inspected, found := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
		if !found {
			return nil, fmt.Errorf("reading inspect's result: ResultOf holds %T", pass.ResultOf[inspect.Analyzer])
		}
		inspected.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(node ast.Node) {
			function, isFunction := node.(*ast.FuncDecl)
			if !isFunction || function.Name.Name != "Target" {
				return
			}
			pass.Report(analysis.Diagnostic{
				Pos:      function.Name.Pos(),
				End:      function.Name.End(),
				Category: "namedTarget",
				Message:  "Target is the function this rule is planted to find",
				SuggestedFixes: []analysis.SuggestedFix{{
					Message: "rename it to Renamed",
					TextEdits: []analysis.TextEdit{
						{Pos: function.Name.Pos(), End: function.Name.End(), NewText: []byte("Renamed")},
					},
				}},
			})
		})
		return nil, nil
	},
}

// packageClauseAnalyzer reports every package clause twice, so the run has to collapse it to one finding.
var packageClauseAnalyzer = &analysis.Analyzer{
	Name: "packageClause",
	Doc:  "packageClause reports every package clause, twice",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			pass.Reportf(file.Name.Pos(), "package %s", file.Name.Name)
			pass.Reportf(file.Name.Pos(), "package %s", file.Name.Name)
		}
		return nil, nil
	},
}

// packageClauseRule is packageClauseAnalyzer ported from vet, so its messageId is its upstream name.
var packageClauseRule = rule.Rule{
	Name:         rule.Namespace + "test-packageClause",
	Origin:       rule.OriginVet,
	UpstreamName: "packageclause",
	Analyzer:     packageClauseAnalyzer,
}

// markedAnalyzer exports a fact for every function named Marked and reports each call, from another package, to
// a function with that fact.
var markedAnalyzer = &analysis.Analyzer{
	Name:      "marked",
	Doc:       "marked reports calls to functions another package marked",
	FactTypes: []analysis.Fact{new(markedFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Name.Name != "Marked" {
					continue
				}
				if defined := pass.TypesInfo.Defs[function.Name]; defined != nil {
					pass.ExportObjectFact(defined, new(markedFact))
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector {
					return true
				}
				callee := pass.TypesInfo.Uses[selector.Sel]
				if callee != nil && callee.Pkg() != pass.Pkg && pass.ImportObjectFact(callee, new(markedFact)) {
					pass.Reportf(call.Pos(), "calls %s, which %s marked", callee.Name(), callee.Pkg().Name())
				}
				return true
			})
		}
		return nil, nil
	},
}

// plantedPanic panics, from a frame of its own, so a crash can be checked for naming where the panic was raised.
func plantedPanic(pass *analysis.Pass) {
	panic(fmt.Sprintf("planted panic in %s", pass.Pkg.Name()))
}

// panickingAnalyzer panics on every package.
var panickingAnalyzer = &analysis.Analyzer{
	Name: "panicking",
	Doc:  "panicking panics",
	Run: func(pass *analysis.Pass) (any, error) {
		plantedPanic(pass)
		return nil, nil
	},
}

// panickingPrerequisite panics on every package, and is no rule: only dependentAnalyzer requires it.
var panickingPrerequisite = &analysis.Analyzer{
	Name: "panickingPrerequisite",
	Doc:  "panickingPrerequisite panics",
	Run: func(pass *analysis.Pass) (any, error) {
		panic("planted prerequisite panic")
	},
}

// dependentAnalyzer finds nothing and requires panickingPrerequisite, so it can never run.
var dependentAnalyzer = &analysis.Analyzer{
	Name:     "dependent",
	Doc:      "dependent requires an analyzer that panics",
	Requires: []*analysis.Analyzer{panickingPrerequisite},
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, nil
	},
}

// erroringAnalyzer returns an error on every package.
var erroringAnalyzer = &analysis.Analyzer{
	Name: "erroring",
	Doc:  "erroring fails",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("reading %s: planted failure", pass.Pkg.Name())
	},
}

// quietAnalyzer runs and finds nothing.
var quietAnalyzer = &analysis.Analyzer{
	Name: "quiet",
	Doc:  "quiet finds nothing",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, nil
	},
}

// nestingAnalyzer reports, once per package, how many recovering wrappers are on the stack it runs on.
var nestingAnalyzer = &analysis.Analyzer{
	Name: "nesting",
	Doc:  "nesting reports how many times its Run is wrapped",
	Run: func(pass *analysis.Pass) (any, error) {
		callers := make([]uintptr, 64)
		frames := runtime.CallersFrames(callers[:runtime.Callers(0, callers)])
		wrappers := 0
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, recoveringRunFunction) {
				wrappers++
			}
			if !more {
				break
			}
		}
		pass.Reportf(pass.Files[0].Package, "wrapped %d times", wrappers)
		return nil, nil
	},
}

// fixturePath is the absolute path of a fixture file named relative to the module.
func fixturePath(root, relative string) string {
	return filepath.Join(root, filepath.FromSlash(relative))
}

// A diagnostic comes back with its rule, absolute file, 1-based byte line and column, exclusive end, the
// Category as its messageId, its message, and a fix whose edit's offsets cut out exactly the name it renames.
func TestADiagnosticComesBackWithEveryField(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(targetAnalyzer))
	contents := fixtureFiles["base/base.go"]
	start := strings.Index(contents, "Target")
	want := []Finding{{
		Rule:      rule.Namespace + "test-target",
		File:      fixturePath(root, "base/base.go"),
		Line:      6,
		Column:    15,
		EndLine:   6,
		EndColumn: 21,
		MessageId: "namedTarget",
		Message:   "Target is the function this rule is planted to find",
		Suggestions: []Suggestion{{
			Message: "rename it to Renamed",
			Edits:   []Edit{{File: fixturePath(root, "base/base.go"), Start: start, End: start + 6, Text: "Renamed"}},
		}},
	}}
	if !reflect.DeepEqual(result.Findings, want) {
		test.Fatalf("want\n%+v\ngot\n%+v", want, result.Findings)
	}
	if edited := contents[start : start+6]; edited != "Target" {
		test.Fatalf("the edit's offsets cut %q out of the file, not Target", edited)
	}
	if len(result.Crashes) > 0 {
		test.Fatalf("want no crashes, got %+v", result.Crashes)
	}
}

// Two rules over the same packages each come back under their own name, a ported rule's messageId is its
// upstream name, a diagnostic reported twice is one finding, and the findings are sorted by file, line,
// column and rule.
func TestTwoRulesOnOnePackageEachKeepTheirName(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(targetAnalyzer), packageClauseRule)
	var named []string
	for _, found := range result.Findings {
		relative, relativeError := filepath.Rel(root, found.File)
		if relativeError != nil {
			test.Fatalf("relating %s to %s: %v", found.File, root, relativeError)
		}
		named = append(named, fmt.Sprintf("%s:%d:%d %s/%s", filepath.ToSlash(relative), found.Line, found.Column,
			found.Rule, found.MessageId))
	}
	want := []string{
		"base/base.go:1:9 cohere-go/test-packageClause/packageclause",
		"base/base.go:6:15 cohere-go/test-target/namedTarget",
		"user/user.go:1:9 cohere-go/test-packageClause/packageclause",
	}
	if !slices.Equal(named, want) {
		test.Fatalf("want\n%q\ngot\n%q", want, named)
	}
	wantCounts := map[string]int{rule.Namespace + "test-target": 1, rule.Namespace + "test-packageClause": 2}
	if !reflect.DeepEqual(result.FindingsByRule, wantCounts) {
		test.Fatalf("want the findings by rule %v, got %v", wantCounts, result.FindingsByRule)
	}
}

// inspect.Analyzer is wrapped in place, so the result a rule reads through ResultOf, keyed by the original
// pointer, is still there: targetAnalyzer refuses to run without it, which would be a crash.
func TestARuleReadsWhatItRequiresThroughResultOfAfterTheWrap(test *testing.T) {
	test.Parallel()

	result := runRules(test, Options{}, houseRule(targetAnalyzer))
	if len(result.Crashes) > 0 || len(result.Findings) != 1 {
		test.Fatalf("want the one finding and no crash, got %+v and %+v", result.Findings, result.Crashes)
	}
	wrapped.mutex.Lock()
	inspectWrapped := wrapped.analyzers[inspect.Analyzer]
	wrapped.mutex.Unlock()
	if !inspectWrapped {
		test.Fatalf("inspect.Analyzer was never wrapped, so this proves nothing about the wrap")
	}
}

// A rule that panics is a crash on each package, naming the rule, the analyzer, the package, the panic value and
// the frame that panicked, and the other rule's finding still comes back, from a run that did not end the process.
func TestAPanickingRuleIsACrashAndTheOthersStillReport(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(targetAnalyzer), houseRule(panickingAnalyzer))
	if len(result.Findings) != 1 || result.Findings[0].Rule != rule.Namespace+"test-target" {
		test.Fatalf("want target's finding, got %+v", result.Findings)
	}
	if len(result.Crashes) != 2 {
		test.Fatalf("want one crash for each package, got %+v", result.Crashes)
	}
	for index, packageName := range []string{"base", "user"} {
		crash := result.Crashes[index]
		if crash.File != fixturePath(root, packageName+"/"+packageName+".go") {
			test.Errorf("want the crash on %s to name its first file, got %s", packageName, crash.File)
		}
		for _, expected := range []string{
			"cohere-go/test-panicking: running analyzer panicking on example.com/fixture/" + packageName,
			"it panicked with planted panic in " + packageName,
			"check.plantedPanic (",
			"check_test.go:",
		} {
			if !strings.Contains(crash.Error, expected) {
				test.Errorf("want the crash on %s to say %q, got %q", packageName, expected, crash.Error)
			}
		}
		if strings.Contains(crash.Error, "checker.") {
			test.Errorf("want the frames to stop at the wrapper, got %q", crash.Error)
		}
	}
	if result.FindingsByRule[rule.Namespace+"test-panicking"] != 0 {
		test.Errorf("want no findings counted for the rule that crashed, got %v", result.FindingsByRule)
	}
}

// A rule whose prerequisite panics cannot run, and is a crash that names the prerequisite and what it said,
// rather than a hang or the end of the process.
func TestAPanickingPrerequisiteIsACrashOfTheRuleThatRequiresIt(test *testing.T) {
	test.Parallel()

	result := runRules(test, Options{}, houseRule(dependentAnalyzer))
	if len(result.Crashes) != 2 {
		test.Fatalf("want one crash for each package, got %+v", result.Crashes)
	}
	want := "cohere-go/test-dependent: failed prerequisites: panickingPrerequisite@example.com/fixture/base, " +
		"because panickingPrerequisite@example.com/fixture/base: running analyzer panickingPrerequisite on " +
		"example.com/fixture/base: it panicked with planted prerequisite panic, at "
	if !strings.HasPrefix(result.Crashes[0].Error, want) {
		test.Fatalf("want the crash to open with\n%q\ngot\n%q", want, result.Crashes[0].Error)
	}
}

// An analyzer that returns an error is a crash that says the error.
func TestAnAnalyzerErrorIsACrash(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(erroringAnalyzer))
	want := []Crash{
		{File: fixturePath(root, "base/base.go"), Error: "cohere-go/test-erroring: reading base: planted failure"},
		{File: fixturePath(root, "user/user.go"), Error: "cohere-go/test-erroring: reading user: planted failure"},
	}
	if !reflect.DeepEqual(result.Crashes, want) {
		test.Fatalf("want\n%+v\ngot\n%+v", want, result.Crashes)
	}
}

// A fact base exports reaches user, which imports it, and a rule with no Category and no upstream name has its
// analyzer's name as its messageId.
func TestAFactExportedFromADependencyReachesThePackageThatImportsIt(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(markedAnalyzer))
	if len(result.Crashes) > 0 {
		test.Fatalf("want no crashes, got %+v", result.Crashes)
	}
	if len(result.Findings) != 1 {
		test.Fatalf("want one finding, got %+v", result.Findings)
	}
	found := result.Findings[0]
	if found.File != fixturePath(root, "user/user.go") || found.Line != 6 || found.MessageId != "marked" ||
		found.Message != "calls Marked, which base marked" {
		test.Fatalf("want the call in user/user.go:6 reported as marked by base, got %+v", found)
	}
}

// Two runs in one process wrap each analyzer once: a second wrap would nest, and the analyzer would see two
// wrappers on its stack.
func TestRunningTwiceWrapsOnce(test *testing.T) {
	test.Parallel()

	for run := range 2 {
		result := runRules(test, Options{}, houseRule(nestingAnalyzer))
		for _, found := range result.Findings {
			if found.Message != "wrapped 1 times" {
				test.Fatalf("run %d: want the analyzer wrapped once, got %q", run+1, found.Message)
			}
		}
		if len(result.Findings) != 2 {
			test.Fatalf("run %d: want one finding for each package, got %+v", run+1, result.Findings)
		}
	}
}

// A single-threaded run and a parallel one give the same findings, crashes and counts.
func TestSingleThreadedAndParallelRunsAgree(test *testing.T) {
	test.Parallel()

	rules := []rule.Rule{
		houseRule(targetAnalyzer), packageClauseRule, houseRule(markedAnalyzer), houseRule(panickingAnalyzer),
		houseRule(erroringAnalyzer), houseRule(quietAnalyzer),
	}
	singleThreaded := runRules(test, Options{SingleThreaded: true}, rules...)
	parallel := runRules(test, Options{}, rules...)
	singleThreaded.Elapsed, parallel.Elapsed = 0, 0
	if !reflect.DeepEqual(singleThreaded, parallel) {
		test.Fatalf("single-threaded\n%+v\nparallel\n%+v", singleThreaded, parallel)
	}
	if len(parallel.Findings) == 0 || len(parallel.Crashes) == 0 {
		test.Fatalf("want findings and crashes to compare, got %+v", parallel)
	}
}

// The counts the lint record reads: every rule run, each file once, the nodes of one walk over those files,
// and a rule that found nothing listed with 0.
func TestTheCountsDescribeTheRun(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	result := runRules(test, Options{}, houseRule(targetAnalyzer), houseRule(quietAnalyzer))
	if result.RulesRun != 2 {
		test.Errorf("want 2 rules run, got %d", result.RulesRun)
	}
	if result.FilesWalked != 2 {
		test.Errorf("want 2 files walked, got %d", result.FilesWalked)
	}
	nodes := 0
	for _, relative := range []string{"base/base.go", "user/user.go"} {
		path := fixturePath(root, relative)
		parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", relative, parseError)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if node != nil {
				nodes++
			}
			return true
		})
	}
	if result.NodesVisited != nodes {
		test.Errorf("want the %d nodes of the two files, got %d", nodes, result.NodesVisited)
	}
	quietCount, listed := result.FindingsByRule[rule.Namespace+"test-quiet"]
	if !listed || quietCount != 0 {
		test.Errorf("want the quiet rule listed with 0 findings, got %v", result.FindingsByRule)
	}
	if result.Elapsed <= 0 {
		test.Errorf("want the elapsed time, got %s", result.Elapsed)
	}
}

// A rule with no analyzer, and two rules sharing one, are refused before anything runs, since no finding could
// say which rule it is.
func TestRulesThatCannotBeTiedToTheirFindingsAreRefused(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	for _, refused := range []struct {
		name   string
		rules  []rule.Rule
		saying string
	}{
		{
			name:   "no analyzer",
			rules:  []rule.Rule{{Name: rule.Namespace + "test-empty", Origin: rule.OriginHouse}},
			saying: "has no analyzer",
		},
		{
			name: "a shared analyzer",
			rules: []rule.Rule{
				houseRule(quietAnalyzer),
				{Name: rule.Namespace + "test-again", Origin: rule.OriginHouse, Analyzer: quietAnalyzer},
			},
			saying: "share analyzer quiet",
		},
	} {
		test.Run(refused.name, func(test *testing.T) {
			test.Parallel()

			_, runError := Run(fixtures.loaded.Packages, refused.rules, Options{})
			if runError == nil || !strings.Contains(runError.Error(), refused.saying) {
				test.Fatalf("want a refusal saying %q, got %v", refused.saying, runError)
			}
		})
	}
}
