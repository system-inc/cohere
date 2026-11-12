package check

import (
	"fmt"
	"go/ast"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
)

// purityProbe reports each static call to another package's function with its purity, as function_purity answers it.
var purityProbe = &analysis.Analyzer{
	Name:     "purityProbe",
	Doc:      "reports each call to another package's function with whether function_purity holds it pure",
	Requires: []*analysis.Analyzer{function_purity.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		purity, isPurity := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
		if !isPurity {
			return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
		}
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if callee := typeutil.StaticCallee(pass.TypesInfo, call); callee != nil && callee.Pkg() != pass.Pkg {
					pass.Reportf(call.Pos(), "%s pure %t", callee.FullName(), purity.IsPure(callee))
				}
				return true
			})
		}
		return nil, nil
	},
}

// answerModule writes a module of two packages to a directory of the test's own: bottom, with a pure Double, and top,
// whose body is topBody, and loads it.
func answerModule(test *testing.T, directory string, topBody string) []*packages.Package {
	test.Helper()
	files := map[string]string{
		"go.mod":           "module example.com/answers\n\ngo 1.24\n",
		"bottom/bottom.go": "package bottom\n\nfunc Double(value int) int {\n\treturn value * 2\n}\n",
		"top/top.go":       "package top\n\nimport \"example.com/answers/bottom\"\n\n" + topBody,
	}
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the module: %v %+v", loadError, loaded)
	}
	return loaded.Packages
}

// answeredRun is a run over the module with top calling bottom.Double, after a cold run over it with top calling
// nothing that writes bottom's entry with only the answers asked (see Caching's AnswersInPart), so it holds no answer
// of Double, with bottom marked for reload before the restore when before is set, and after it otherwise, as a load
// that did not check it from source leaves it.
func answeredRun(test *testing.T, before bool) *Result {
	test.Helper()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(purityProbe)}
	caching := testCaching(test, rules)
	caching.AnswersInPart = map[string]bool{"example.com/answers/bottom": true}
	runCached(test, answerModule(test, directory, "var Limit = 3\n\nvar _ = bottom.Double\n"), caching, rules)
	caching.AnswersInPart = nil
	analyzed := answerModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	var reachable []*packages.Package
	packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
	session := NewCacheSession(caching, rules)
	if before {
		session.Reload([]string{"example.com/answers/bottom"})
	}
	session.Restore(load.LoaderSource, reachable, analyzed)
	if !before {
		session.Reload([]string{"example.com/answers/bottom"})
	}
	result, runError := Run(analyzed, rules, Options{Cache: session})
	if runError != nil {
		test.Fatalf("running the probe: %v", runError)
	}
	return result
}

// A package marked for reload is never restored, so the run learns it and answers from its source, and its entry,
// written again, holds the answer.
func TestAPackageMarkedForReloadIsCheckedFromSource(test *testing.T) {
	test.Parallel()
	result := answeredRun(test, true)
	if len(result.Crashes) > 0 || len(result.Reloads) > 0 || result.Cache.Reloads != 1 ||
		result.Cache.ReloadRounds != 1 {
		test.Fatalf("want bottom checked from source and nothing more reloaded, got crashes %+v, reloads %v, %+v",
			result.Crashes, result.Reloads, result.Cache)
	}
	if len(result.Findings) != 1 || result.Findings[0].Message != "example.com/answers/bottom.Double pure true" {
		test.Fatalf("want bottom.Double answered pure, got %+v", result.Findings)
	}
}

// A reload that fails, a package the run marked for reload and still restored, so it never learned it, is a gap the
// run names: the question fails the pass that asked it, naming the package, and asks for no reload again, which could
// change nothing. It is never answered no fact.
func TestAReloadThatFailsIsANamedGap(test *testing.T) {
	test.Parallel()
	result := answeredRun(test, false)
	if len(result.Reloads) > 0 || len(result.Findings) > 0 {
		test.Fatalf("want no reload asked again and no finding, got reloads %v, findings %+v", result.Reloads,
			result.Findings)
	}
	const named = "asked function_purity of example.com/answers/bottom.Double, of example.com/answers/bottom, which " +
		"the run loaded from source to answer it and still never learned, so it has no answer"
	if len(result.Crashes) != 1 || !strings.Contains(result.Crashes[0].Error, named) {
		test.Fatalf("want one crash naming bottom's failed reload, got %+v", result.Crashes)
	}
}

// A pass whose question reached a restored package with no answer stored asks for it to be loaded again, and the round
// that loads it is counted under missingAnswer, so an extra build's record shows a reload no decline of the host's
// restore asked for.
func TestAReloadRoundForAMissingAnswerIsCountedByItsReason(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(purityProbe)}
	caching := testCaching(test, rules)
	caching.AnswersInPart = map[string]bool{"example.com/answers/bottom": true}
	runCached(test, answerModule(test, directory, "var Limit = 3\n\nvar _ = bottom.Double\n"), caching, rules)
	caching.AnswersInPart = nil
	session := NewCacheSession(caching, rules)
	pass := func() *Result {
		analyzed := answerModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
		var reachable []*packages.Package
		packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
		session.Restore(load.LoaderSource, reachable, analyzed)
		result, runError := Run(analyzed, rules, Options{Cache: session})
		if runError != nil {
			test.Fatalf("running the probe: %v", runError)
		}
		return result
	}
	first := pass()
	if !slices.Equal(first.Reloads, []string{"example.com/answers/bottom"}) || !session.Reload(first.Reloads) {
		test.Fatalf("want the first pass to ask for bottom to be loaded again, got %v", first.Reloads)
	}
	again := pass()
	want := map[string]int{reloadForMissingAnswer: 1}
	if len(again.Reloads) > 0 || again.Cache.ReloadRounds != 1 || !maps.Equal(again.Cache.ReloadRoundsBy, want) {
		test.Fatalf("want one round, counted under %v, and nothing more to load, got %d rounds by %v, reloads %v",
			want, again.Cache.ReloadRounds, again.Cache.ReloadRoundsBy, again.Reloads)
	}
}

// An entry the first wave wrote with only the answers it asked, rewritten once the second wave asked more (see
// rewriteEarlier), is a record whole for the next run (#fq7dxvx): it holds its new body and names the export data it
// named before, so with top edited the run hits bottom, reads its types from that export data and its answer from the
// rewritten body, and asks for no reload.
func TestAnEntryRewrittenWithItsAnswersIsReadWhole(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(purityProbe)}
	caching := testCaching(test, rules)
	caching.AnswersInPart = map[string]bool{"example.com/answers/bottom": true}
	answerModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	cold := runWithSession(test, directory, caching, rules)
	bottomKey := sessionKey(test, cold.session, "example.com/answers/bottom")
	written, _, stored := storedEntryAt(caching.Store, bottomKey)
	if !stored || answerCount(written.Answers) == 0 {
		test.Fatalf("want bottom's entry rewritten with Double's answer, got %+v (stored %t)", written.Answers, stored)
	}

	caching.AnswersInPart = nil
	answerModule(test, directory, "func Use() int {\n\treturn bottom.Double(3)\n}\n")
	edited := runWithSession(test, directory, caching, rules)
	if edited.result.Cache.Hit != 1 || edited.result.Cache.Reloads != 0 || edited.loaded.PackagesFromExportData != 1 {
		test.Fatalf("want bottom hit, read from its export data, with no reload, got %d read from export data and "+
			"%+v", edited.loaded.PackagesFromExportData, edited.result.Cache)
	}
	if len(edited.result.Findings) != 1 ||
		edited.result.Findings[0].Message != "example.com/answers/bottom.Double pure true" {
		test.Fatalf("want bottom.Double answered pure, got %+v", edited.result.Findings)
	}
}
