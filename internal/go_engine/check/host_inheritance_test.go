package check

import (
	"cmp"
	"go/ast"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// inheritFiles are a module of one package, shared, with two files every build compiles, one only windows builds, and
// an internal test, so the host's run holds shared.go in both shared and `shared [shared.test]`, and windows's build
// checks both of them from source, since a file of each differs there.
var inheritFiles = map[string]string{
	"go.mod":                   "module example.com/inherit\n\ngo 1.24\n",
	"shared/shared.go":         "// Package shared is held by two packages of the host's run.\npackage shared\n\nfunc Shared() {}\n",
	"shared/other.go":          "package shared\n\nfunc Other() {}\n",
	"shared/shared_windows.go": "package shared\n\nfunc Windows() {}\n",
	"shared/shared_test.go":    "package shared\n\nimport \"testing\"\n\nfunc TestShared(test *testing.T) { Shared() }\n",
}

// inheritCounts is each syntax-only test rule's calls, by the base name of the file it was called on, over a run.
type inheritCounts struct {
	lock  sync.Mutex
	calls map[string]map[string]int
}

// inheritRules are two syntax-only rules, each counting into counts the files it is handed: walking, whose own analyzer
// walks pass.Files and reports each file's package name with a fix that writes it again, and listening, a dispatched
// listener that reports each file's package clause.
func inheritRules(counts *inheritCounts) []rule.Rule {
	count := func(ruleName string, filename string) {
		counts.lock.Lock()
		defer counts.lock.Unlock()
		if counts.calls[ruleName] == nil {
			counts.calls[ruleName] = map[string]int{}
		}
		counts.calls[ruleName][filepath.Base(filename)]++
	}
	walking := &analysis.Analyzer{
		Name: "walking",
		Doc:  "reports each file's package name with a fix",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				tokenFile := pass.Fset.File(file.FileStart)
				count("walking", tokenFile.Name())
				pass.Report(analysis.Diagnostic{Pos: file.Name.Pos(), End: file.Name.End(), Category: "named",
					Message: "named " + file.Name.Name, SuggestedFixes: []analysis.SuggestedFix{{
						Message: "name it again",
						TextEdits: []analysis.TextEdit{
							{Pos: file.Name.Pos(), End: file.Name.End(), NewText: []byte("shared")},
						},
					}}})
			}
			return nil, nil
		},
	}
	walkingRule := houseRule(walking)
	// As the registry marks a syntax-only walk.
	walking.Run = rule.SyntaxOnlyRun(walkingRule.Name, walking.Run)
	listener := &rule.Listener{
		FileLocal:  true,
		SyntaxOnly: true,
		FileEnter: func(context *rule.Context, file *ast.File) {
			count("listening", context.FileSet.File(file.FileStart).Name())
			context.Reportf(file.Package, "a clause")
		},
	}
	listeningRule := houseRule(dispatch.NewSyntax().RuleAnalyzer(rule.Namespace+"test-listening", "listening",
		"reports each file's package clause", listener))
	listeningRule.Listener = listener
	return []rule.Rule{walkingRule, listeningRule}
}

// inheritHost is the host's run over root with rules, keeping its packages for the builds.
func inheritHost(test *testing.T, root string, rules []rule.Rule) *HostRun {
	test.Helper()
	session := NewCacheSession(cachingAt(test.TempDir(), rules), rules)
	session.KeepForBuilds()
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, RecordReads: load.EveryPackage})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the host's build: %v %+v", loadError, loaded.Diagnostics)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots, KeepForBuilds: true})
	if runError != nil || len(result.Crashes) > 0 {
		test.Fatalf("running the host's build: %v %+v", runError, result.Crashes)
	}
	result.ReadWalks.Wait()
	return result.Host
}

// inheritBuild is windows's run over root with rules, restoring from host, inheriting through inheritance when it is
// not nil, and its findings sorted.
func inheritBuild(
	test *testing.T,
	root string,
	rules []rule.Rule,
	host *HostRun,
	inheritance *HostInheritance,
) []Finding {
	test.Helper()
	caching := cachingAt(test.TempDir(), rules)
	caching.Context.Build = append(caching.Context.Build, "GOOS=windows")
	session := NewCacheSession(caching, rules)
	session.RestoreFromHost(host)
	options := Options{Cache: session}
	// A nil *HostInheritance would be a non-nil rule.Inheritance.
	if inheritance != nil {
		options.Inheritance = inheritance
	}
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"},
			nil, load.Caching{Restorer: session})
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading windows's build: %v %+v", loadError, loaded.Diagnostics)
		}
		options.Withheld, options.Bodiless = loaded.Withheld, loaded.Bodiless
		result, runError := Run(loaded.Packages, rules, options)
		if runError != nil || len(result.Crashes) > 0 {
			test.Fatalf("running windows's build: %v %+v", runError, result.Crashes)
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			return slices.SortedFunc(slices.Values(result.Findings), func(left Finding, right Finding) int {
				return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Rule, right.Rule),
					cmp.Compare(left.Line, right.Line), cmp.Compare(left.Column, right.Column))
			})
		}
	}
}

// A file two of the host's packages hold, shared and `shared [shared.test]`, is inherited from either alike (#y6q1x2j):
// the host's findings of each syntax-only rule on shared.go are the same from the lowest ID and from the highest, and
// windows's build, which checks both packages from source, finds the same inheriting from either as it does running
// every rule on every file. Inheriting, neither rule, own walk or listener, is handed shared.go, other.go or
// shared_test.go, which the host checked, and both are handed shared_windows.go, which the host never compiled: three
// files inherited, each with one finding of each rule.
func TestAFileTwoHostPackagesHoldIsInheritedFromEitherAlike(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeFiles(test, root, inheritFiles)
	counts := &inheritCounts{calls: map[string]map[string]int{}}
	rules := inheritRules(counts)
	host := inheritHost(test, root, rules)
	sharedFile := filepath.Join(root, "shared", "shared.go")
	if holders := host.holdersOf(sharedFile); len(holders) != 2 {
		test.Fatalf("the host kept %q holding shared.go, want shared and its test variant", holders)
	}

	counts.calls = map[string]map[string]int{}
	whole := inheritBuild(test, root, rules, host, nil)
	if counts.calls["walking"]["shared.go"] == 0 || counts.calls["listening"]["shared.go"] == 0 {
		test.Fatalf("without inheritance the rules were handed %v, want shared.go among them", counts.calls)
	}
	byHolder := map[bool][]rule.InheritedFinding{}
	for _, fromHighest := range []bool{false, true} {
		inheritance := host.Inheritance(rules)
		inheritance.fromHighest = fromHighest
		counts.calls = map[string]map[string]int{}
		inherited := inheritBuild(test, root, rules, host, inheritance)
		if !reflect.DeepEqual(inherited, whole) {
			test.Errorf("inheriting from the highest holder %t found\n%+v\nwhere every rule on every file found\n%+v",
				fromHighest, inherited, whole)
		}
		for _, ruleName := range []string{"walking", "listening"} {
			handed := counts.calls[ruleName]
			if len(handed) != 1 || handed["shared_windows.go"] == 0 {
				test.Errorf("inheriting from the highest holder %t, %s was handed %v, want shared_windows.go alone",
					fromHighest, ruleName, handed)
			}
		}
		if files, findings := inheritance.Counts(); files != 3 || findings != 6 {
			test.Errorf("inheriting from the highest holder %t counted %d files and %d findings, want 3 and 6",
				fromHighest, files, findings)
		}
		byHolder[fromHighest] = inheritance.Findings(rules[0].Name, sharedFile)
	}
	if len(byHolder[false]) != 1 || !reflect.DeepEqual(byHolder[false], byHolder[true]) {
		test.Errorf("shared.go's findings from the lowest holder\n%+v\nand from the highest\n%+v\nwant one, alike",
			byHolder[false], byHolder[true])
	}
}
