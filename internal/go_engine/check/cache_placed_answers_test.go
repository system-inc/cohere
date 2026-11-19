package check

import (
	"bytes"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// placedKind is the kind placedProbe asks, a boolean registered nowhere, so an entry's Answers computed whole never
// hold it and only its placed answers, and those recorded by objectpath, keep it.
var placedKind = &fact_resolver.Kind{Name: "placed_probe"}

// placedProbe asks placedKind of each static call's callee, every answer true, and reports the callee with its answer.
var placedProbe = &analysis.Analyzer{
	Name:     "placedProbe",
	Doc:      "reports each static call's callee with the placed_probe answer about it, every one computed true",
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		resolver, resolverError := fact_resolver.Of(pass)
		if resolverError != nil {
			return nil, resolverError
		}
		reader := resolver.Package(pass.Pkg)
		if reader == nil {
			return nil, fmt.Errorf("the resolver never learned %s, which ctrlflow ran on", pass.Pkg.Path())
		}
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if callee := typeutil.StaticCallee(pass.TypesInfo, call); callee != nil && callee.Pkg() != nil {
					answer := fact_resolver.Ask(resolver, reader, placedKind, callee,
						func(*fact_resolver.Package) bool { return true }, func() bool { return false })
					pass.Reportf(call.Pos(), "%s placed %t", callee.FullName(), answer)
				}
				return true
			})
		}
		return nil, nil
	},
}

// placedBottom is bottom's file in placedModule: Double calls the unexported twice, which no objectpath names.
const placedBottom = "package bottom\n\nfunc Double(value int) int {\n\treturn twice(value)\n}\n\n" +
	"func twice(value int) int {\n\treturn value * 2\n}\n"

// placedModule writes a module of two packages to directory: bottom, placedBottom, and top, whose body is topBody,
// and loads it.
func placedModule(test *testing.T, directory string, topBody string) []*packages.Package {
	test.Helper()
	files := map[string]string{
		"go.mod":           "module example.com/placed\n\ngo 1.24\n",
		"bottom/bottom.go": placedBottom,
		"top/top.go":       "package top\n\nimport \"example.com/placed/bottom\"\n\n" + topBody,
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

// placedProbeAnswers is entry's placed answers of placedKind, each as its file, the function its offset in bottom's
// file opens, its parameter and its answer, for a test to compare.
func placedProbeAnswers(test *testing.T, entry cacheEntry, bottomFile string) []string {
	test.Helper()
	var named []string
	for _, answer := range entry.Placed {
		if answer.Kind != placedKind.Name {
			continue
		}
		at := "elsewhere"
		for _, function := range []string{"Double", "twice"} {
			if answer.File == bottomFile && answer.Offset == strings.Index(placedBottom, "func "+function+"(") {
				at = function
			}
		}
		named = append(named, fmt.Sprintf("%s %d %s", at, answer.Parameter, answer.Encoded))
	}
	return named
}

// bottomFileOf is the name bottom's one file has in loaded, as its syntax names it.
func bottomFileOf(test *testing.T, loaded []*packages.Package) string {
	test.Helper()
	for _, each := range loaded {
		if each.PkgPath == "example.com/placed/bottom" && len(each.CompiledGoFiles) == 1 {
			return each.CompiledGoFiles[0]
		}
	}
	test.Fatal("the load holds no bottom with one file")
	return ""
}

// A cold run with a store writes bottom's entry with its answers computed whole, every kind they hold complete, and
// beside them, by place, the answer bottom's own analysis asked of twice: unexported, so no objectpath names it, and of
// placed_probe, which no whole computes, so only its place keeps it.
func TestAWholeEntryHoldsAnUnexportedFunctionsAnswerOfAnUnregisteredKindByPlace(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	loaded := placedModule(test, directory, "var _ = bottom.Double\n")
	cold := runWithSession(test, directory, caching, rules)
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, "example.com/placed/bottom"))
	if !stored || len(entry.Answers) == 0 {
		test.Fatalf("want bottom's entry written with its answers computed whole, got %+v (stored %t)", entry.Answers,
			stored)
	}
	for _, kind := range entry.Answers {
		if !kind.Complete || kind.Kind == placedKind.Name {
			test.Fatalf("want every kind of bottom's answers a registered one, complete, got %+v", entry.Answers)
		}
	}
	want := []string{"twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFileOf(test, loaded)); !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry to hold %q by place, got %q", want, placed)
	}
}

// A run that reloads bottom to answer top's question of Double writes bottom's entry again with Double's placed answer
// beside twice's, which the cold run wrote: bottom is not analyzed this time, so nothing asks of twice, and only the
// entry held before keeps its answer, which, of bottom's one key, still holds.
func TestAReloadedEntryKeepsThePlacedAnswersItHeld(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	runCached(test, placedModule(test, directory, "var _ = bottom.Double\n"), caching, rules)
	session := NewCacheSession(caching, rules)
	var bottomFile string
	pass := func() *Result {
		loaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
		bottomFile = bottomFileOf(test, loaded)
		var analyzed []*packages.Package
		for _, each := range loaded {
			if each.PkgPath == "example.com/placed/top" {
				analyzed = append(analyzed, each)
			}
		}
		var reachable []*packages.Package
		packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
		session.Restore(load.LoaderSource, loadedSizes(reachable), reachable, analyzed)
		result, runError := Run(analyzed, rules, Options{Cache: session})
		if runError != nil {
			test.Fatalf("running the probe: %v", runError)
		}
		return result
	}
	first := pass()
	if !slices.Equal(first.Reloads, []string{"example.com/placed/bottom"}) || !session.Reload(first.Reloads) {
		test.Fatalf("want the first pass to ask for bottom to be loaded again, got %v", first.Reloads)
	}
	again := pass()
	if len(again.Reloads) > 0 || len(again.Crashes) > 0 {
		test.Fatalf("want bottom answered from source, got reloads %v and crashes %+v", again.Reloads, again.Crashes)
	}
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, session, "example.com/placed/bottom"))
	want := []string{"Double -1 true", "twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFile); !stored || !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry written again to hold %q by place, got %q (stored %t)", want, placed, stored)
	}
}

// The first wave writes bottom's entry with twice's placed answer, which bottom's own analysis asked, and the second,
// whose top asks Double, writes it again with Double's beside it (rewriteEarlier): its answers, computed whole, and its
// reads are as they were, so only its placed answers grew.
func TestAnEntryWhosePlacedAnswersGrewAfterTheFirstWaveIsWrittenAgain(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	loaded := placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	cold := runWithSession(test, directory, caching, rules)
	if cold.result.Cache.Waves != 2 {
		test.Fatalf("want bottom analyzed in a first wave and top in a second, got %+v", cold.result.Cache)
	}
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, "example.com/placed/bottom"))
	want := []string{"Double -1 true", "twice -1 true"}
	if placed := placedProbeAnswers(test, entry, bottomFileOf(test, loaded)); !stored || !slices.Equal(placed, want) {
		test.Errorf("want bottom's entry written again to hold %q by place, got %q (stored %t)", want, placed, stored)
	}
}

// placedDisagreeing is every place, its kind, file, offset and parameter, at which left and right both hold an answer
// and the two answers differ.
func placedDisagreeing(left []fact_resolver.PlacedAnswer, right []fact_resolver.PlacedAnswer) []string {
	held := make(map[placedAt][]byte, len(left))
	for _, answer := range left {
		held[placedAt{kind: answer.Kind, file: answer.File, offset: answer.Offset,
			parameter: answer.Parameter}] = answer.Encoded
	}
	var disagreeing []string
	for _, answer := range right {
		at := placedAt{kind: answer.Kind, file: answer.File, offset: answer.Offset, parameter: answer.Parameter}
		if encoded, isHeld := held[at]; isHeld && !bytes.Equal(encoded, answer.Encoded) {
			disagreeing = append(disagreeing, fmt.Sprintf("%s %s:%d %d", at.kind, at.file, at.offset, at.parameter))
		}
	}
	return disagreeing
}
