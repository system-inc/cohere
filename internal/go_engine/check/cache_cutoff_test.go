package check

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/structtag"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// callsAnalyzer exports markFact for every function whose body holds the string literal "mark", and reports each
// call to another package's function with its signature and whether that function has the fact, so an importer's
// finding moves with its dependency's signature and with its dependency's body alike.
var callsAnalyzer = &analysis.Analyzer{
	Name:      "calls",
	Doc:       "calls reports each call to another package's function, with its signature and whether it is marked",
	FactTypes: []analysis.Fact{new(markFact)},
	Run:       runCalls,
}

// runCalls is callsAnalyzer's pass.
func runCalls(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if declaration, isDeclaration := node.(*ast.FuncDecl); isDeclaration && holdsMark(declaration.Body) {
				if defined := pass.TypesInfo.Defs[declaration.Name]; defined != nil {
					pass.ExportObjectFact(defined, new(markFact))
				}
			}
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			callee, isFunction := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
			if !isFunction || callee.Pkg() == pass.Pkg {
				return true
			}
			marked := "unmarked"
			if pass.ImportObjectFact(callee, new(markFact)) {
				marked = "marked"
			}
			pass.Reportf(call.Pos(), "calls %s, %s", types.ObjectString(callee, (*types.Package).Name), marked)
			return true
		})
	}
	return nil, nil
}

// holdsMark reports whether body holds the string literal "mark".
func holdsMark(body *ast.BlockStmt) bool {
	found := false
	if body == nil {
		return found
	}
	ast.Inspect(body, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		found = found || isLiteral && literal.Value == `"mark"`
		return !found
	})
	return found
}

// structtagRule is vet's structtag, whose finding on a duplicated tag names the line of the field it duplicates,
// which can be a field of a struct embedded from another package.
var structtagRule = rule.Rule{
	Name:         rule.Namespace + "structtag",
	Origin:       rule.OriginVet,
	UpstreamName: "structtag",
	Analyzer:     structtag.Analyzer,
}

// cutoffRun runs rules over root with the cache and again without it, over a load of its own, fails the test unless
// the two Results agree, and returns the run with the cache.
func cutoffRun(test *testing.T, step string, root string, caching *Caching, rules []rule.Rule) sessionRun {
	test.Helper()
	cached := runWithSession(test, root, caching, rules)
	uncachedLoad, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("%s: loading %s without the cache: %v", step, root, loadError)
	}
	uncached, runError := Run(uncachedLoad.Packages, rules, Options{})
	if runError != nil {
		test.Fatalf("%s: running without the cache: %v", step, runError)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(cached.result), withoutTimesOrCache(uncached)) {
		test.Fatalf("%s: with the cache\n%+v\nwithout it\n%+v", step, withoutTimesOrCache(cached.result),
			withoutTimesOrCache(uncached))
	}
	return cached
}

// expectCounts fails the test unless the run hit, hit by its second key, missed and analyzed in the waves given.
func expectCounts(test *testing.T, step string, ran sessionRun, hit int, secondKeyHit int, missed int, waves int) {
	test.Helper()
	got := ran.result.Cache
	if got.Hit != hit || got.SecondKeyHit != secondKeyHit || got.Missed != missed || got.Waves != waves {
		test.Fatalf("%s: want %d hit, %d of them by the second key, %d missed in %d waves, got %+v", step, hit,
			secondKeyHit, missed, waves, got)
	}
}

// editFile rewrites the file at name under root with edit, failing the test unless edit changed it.
func editFile(test *testing.T, root string, name string, edit func(string) string) {
	test.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("reading %s: %v", name, readError)
	}
	edited := edit(string(contents))
	if edited == string(contents) {
		test.Fatalf("the edit left %s as it was", name)
	}
	if writeError := os.WriteFile(path, []byte(edited), 0o644); writeError != nil {
		test.Fatalf("writing %s: %v", name, writeError)
	}
}

// replaced is an edit that replaces old, which must be in the file, with new.
func replaced(test *testing.T, old string, new string) func(string) string {
	return func(contents string) string {
		if !strings.Contains(contents, old) {
			test.Fatalf("no %q to replace", old)
		}
		return strings.Replace(contents, old, new, 1)
	}
}

// valueFiles are a module whose user calls base's Value and Later, Later declared below Value's body.
var valueFiles = map[string]string{
	"go.mod": "module example.com/cutoff\n\ngo 1.24\n",
	"base/base.go": "package base\n\n// Value is what user calls.\nfunc Value() int {\n\ttotal := 1\n" +
		"\treturn total\n}\n\n// Later is declared below Value's body, so a line added to it moves Later.\n" +
		"func Later() int { return 2 }\n",
	"user/user.go": "package user\n\nimport \"example.com/cutoff/base\"\n\n" +
		"// Use calls base.\nfunc Use() {\n\t_ = base.Value()\n\t_ = base.Later()\n}\n",
}

// An edit to base that leaves what an importer can read of it as it was misses base alone: a comment at the end, a
// statement added to a body, which moves every declaration below it, a comment near the top, which moves every
// declaration, and a body edit that keeps every line. user is restored by its second key, nothing but base is
// analyzed, in one wave, and the Result is a run's without the cache over the edited tree.
func TestABodyOnlyEditMissesOnlyItsOwnPackage(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, valueFiles)
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	caching := testCaching(test, rules)
	cold := cutoffRun(test, "cold", root, caching, rules)
	expectCounts(test, "cold", cold, 0, 0, 2, 2)
	for _, edit := range []struct {
		step string
		edit func(string) string
	}{
		{"a comment at the end", func(contents string) string { return contents + "\n// A comment at the end.\n" }},
		{"a statement in a body", replaced(test, "\ttotal := 1\n", "\ttotal := 1\n\ttotal++\n")},
		{"a comment near the top", replaced(test, "package base\n", "package base\n\n// A comment near the top.\n")},
		{"a body edit that keeps every line", replaced(test, "total := 1", "total := 3")},
	} {
		editFile(test, root, "base/base.go", edit.edit)
		edited := cutoffRun(test, edit.step, root, caching, rules)
		expectCounts(test, edit.step, edited, 1, 1, 1, 1)
	}
}

// An edit to base's signature reaches every package above it: user and top, which imports only user, both miss and
// say the new signature, and are analyzed together in the second wave however deep they go.
func TestASignatureEditReachesEveryImporter(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, valueFiles)
	writeFiles(test, root, map[string]string{
		"top/top.go": "package top\n\nimport \"example.com/cutoff/user\"\n\n" +
			"// Top calls user.\nfunc Top() {\n\tuser.Use()\n}\n",
	})
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	caching := testCaching(test, rules)
	cutoffRun(test, "cold", root, caching, rules)
	editFile(test, root, "base/base.go", replaced(test, "func Value() int {\n\ttotal := 1",
		"func Value() int64 {\n\tvar total int64 = 1"))
	edited := cutoffRun(test, "the signature edit", root, caching, rules)
	expectCounts(test, "the signature edit", edited, 0, 0, 3, 2)
	said := false
	for _, found := range edited.result.Findings {
		said = said || found.Message == "calls func base.Value() int64, unmarked"
	}
	if !said {
		test.Fatalf("want user's call reported with the new signature, got %+v", edited.result.Findings)
	}
}

// An edit to base's body alone that changes a fact it exports, its export data as it was, misses user, whose
// finding says the new fact.
func TestABodyEditThatChangesAFactReachesTheImporter(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, valueFiles)
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	caching := testCaching(test, rules)
	cutoffRun(test, "cold", root, caching, rules)
	editFile(test, root, "base/base.go", replaced(test, "\ttotal := 1\n", "\ttotal := len(\"mark\")\n"))
	edited := cutoffRun(test, "the fact edit", root, caching, rules)
	expectCounts(test, "the fact edit", edited, 0, 0, 2, 2)
	said := false
	for _, found := range edited.result.Findings {
		said = said || found.Message == "calls func base.Value() int, marked"
	}
	if !said {
		test.Fatalf("want user's call reported as marked, got %+v", edited.result.Findings)
	}
}

// chainFiles are a module whose top calls a method of bottom's type through middle alone, which returns the type, so
// the method's fact reaches top through middle, which neither declares it nor changes when it changes.
var chainFiles = map[string]string{
	"go.mod": "module example.com/through\n\ngo 1.24\n",
	"bottom/bottom.go": "package bottom\n\n// T is what middle returns.\ntype T struct{}\n\n" +
		"// M is what top calls.\nfunc (T) M() int {\n\treturn len(\"mark\")\n}\n",
	"middle/middle.go": "package middle\n\nimport \"example.com/through/bottom\"\n\n" +
		"// New is a bottom.T.\nfunc New() bottom.T { return bottom.T{} }\n",
	"top/top.go": "package top\n\nimport \"example.com/through/middle\"\n\n" +
		"// Use calls bottom's method through middle.\nfunc Use() int { return middle.New().M() }\n",
}

// A fact two levels below a package reaches it though the package between comes out the same, so the second key is
// over everything below a package, not only what it imports. bottom's method loses its fact and gets it back with
// other bytes; on the third run middle is restored by its second key from the first run's entry, and top must be
// too, from the first run's entry, which says marked, not from the second's, which says unmarked.
func TestAFactTwoLevelsDownReachesThroughAnUnchangedPackage(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, chainFiles)
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	caching := testCaching(test, rules)
	cutoffRun(test, "cold", root, caching, rules)
	editFile(test, root, "bottom/bottom.go", replaced(test, "len(\"mark\")", "len(\"none\")"))
	unmarked := cutoffRun(test, "the fact taken away", root, caching, rules)
	expectCounts(test, "the fact taken away", unmarked, 0, 0, 3, 2)
	editFile(test, root, "bottom/bottom.go", replaced(test, "len(\"none\")", "len(\"mark\") + 0"))
	marked := cutoffRun(test, "the fact given back", root, caching, rules)
	expectCounts(test, "the fact given back", marked, 2, 2, 1, 1)
}

// taggedFiles are a module whose user embeds base's T beside a struct of its own with the same json tag at the same
// depth, so structtag's finding on user names the line of base's field.
var taggedFiles = map[string]string{
	"go.mod":       "module example.com/tagged\n\ngo 1.24\n",
	"base/base.go": "package base\n\n// T is embedded by user.\ntype T struct {\n\tX int `json:\"x\"`\n}\n",
	"user/user.go": "package user\n\nimport \"example.com/tagged/base\"\n\n" +
		"// W holds a field tagged as base's is.\ntype W struct {\n\tZ int `json:\"x\"`\n}\n\n" +
		"// U embeds both, so the tag is duplicated at one depth.\ntype U struct {\n\tbase.T\n\tW\n}\n",
}

// A package one of whose findings names a file outside it never takes the second key: a comment that moves base's
// field down a line misses user, whose finding then says the new line, as a run without the cache does.
func TestAFindingNamingAnotherPackagesLineKeepsItsPackageOnTheFirstKey(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, taggedFiles)
	rules := []rule.Rule{structtagRule}
	caching := testCaching(test, rules)
	cold := cutoffRun(test, "cold", root, caching, rules)
	if len(cold.result.Findings) != 1 || !strings.HasSuffix(cold.result.Findings[0].Message, "base/base.go:5") {
		test.Fatalf("want structtag's finding naming base's line 5, got %+v", cold.result.Findings)
	}
	editFile(test, root, "base/base.go", replaced(test, "package base\n", "package base\n\n// Moves T down.\n"))
	edited := cutoffRun(test, "the line moved", root, caching, rules)
	expectCounts(test, "the line moved", edited, 0, 0, 2, 2)
	if !strings.HasSuffix(edited.result.Findings[0].Message, "base/base.go:7") {
		test.Fatalf("want the finding moved to base's line 7, got %+v", edited.result.Findings)
	}
}

// A package the second key restored keeps the export data of its first run, whose positions of base's objects are
// stale once base's lines moved. A later run that misses a package importing it reads that export data, against
// base's own types, so top's finding, which names the line of base's field through middle, is a run's without the
// cache.
func TestAMissOverAPackageTheSecondKeyRestoredReadsItsDependenciesFresh(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod":       "module example.com/stale\n\ngo 1.24\n",
		"base/base.go": taggedFiles["base/base.go"],
		"middle/middle.go": "package middle\n\nimport \"example.com/stale/base\"\n\n" +
			"// M embeds base's T.\ntype M struct {\n\tbase.T\n}\n",
		"top/top.go": "package top\n\nimport \"example.com/stale/middle\"\n\n" +
			"// W holds a field tagged as base's is.\ntype W struct {\n\tZ int `json:\"x\"`\n}\n\n" +
			"// V holds W one level down, as M holds T.\ntype V struct {\n\tW\n}\n\n" +
			"// U embeds both.\ntype U struct {\n\tmiddle.M\n\tV\n}\n",
	})
	rules := []rule.Rule{structtagRule}
	caching := testCaching(test, rules)
	cutoffRun(test, "cold", root, caching, rules)
	editFile(test, root, "base/base.go", replaced(test, "package base\n", "package base\n\n// Moves T down.\n"))
	moved := cutoffRun(test, "base's lines moved", root, caching, rules)
	expectCounts(test, "base's lines moved", moved, 1, 1, 2, 2)
	editFile(test, root, "top/top.go", func(contents string) string { return contents + "\n// Edited.\n" })
	edited := cutoffRun(test, "top edited", root, caching, rules)
	expectCounts(test, "top edited", edited, 2, 0, 1, 1)
	if edited.loaded.PackagesFromExportData != 2 {
		test.Fatalf("want base and middle read from export data, got %d", edited.loaded.PackagesFromExportData)
	}
	if len(edited.result.Findings) != 1 || !strings.HasSuffix(edited.result.Findings[0].Message, "base/base.go:7") {
		test.Fatalf("want top's finding naming base's line 7, got %+v", edited.result.Findings)
	}
}
