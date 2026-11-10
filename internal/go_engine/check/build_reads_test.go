package check

import (
	"fmt"
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// buildReadsFiles are a module whose package user calls two functions of base: Logf, a printf wrapper, which printf's
// port learns a fact about on base, and Plain, which it learns none about, so user's printf reads a fact of base that
// is there and one that is not.
var buildReadsFiles = map[string]string{
	"go.mod": "module example.com/reads\n\ngo 1.21\n",
	"base/base.go": "package base\n\nimport \"fmt\"\n\n// Logf formats as fmt.Sprintf does.\n" +
		"func Logf(format string, arguments ...any) string {\n\treturn fmt.Sprintf(format, arguments...)\n}\n\n" +
		"// Plain is its value.\nfunc Plain(value string) string { return value }\n",
	"user/user.go": "package user\n\nimport \"example.com/reads/base\"\n\n" +
		"// Use calls both.\nfunc Use() string { return base.Logf(\"%d\", 1) + base.Plain(\"x\") }\n",
}

// loadedBuildReads writes buildReadsFiles under a directory of the test's and loads it, failing the test on any
// diagnostic, and returns the load and its packages by import path.
func loadedBuildReads(test *testing.T) (*load.Loaded, map[string]*packages.Package) {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, buildReadsFiles)
	loaded := loadRecordingAt(test, directory)
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("the fixture loaded with diagnostics: %+v", loaded.Diagnostics)
	}
	byPath := map[string]*packages.Package{}
	for _, analyzed := range loaded.Packages {
		byPath[analyzed.PkgPath] = analyzed
	}
	return loaded, byPath
}

// declaredAtAnalyzer reports, on every file, the file's own name, and at every call of a function named Logf from
// another package, the file and line that declare it, as vet's structtag names another package's position.
var declaredAtAnalyzer = &analysis.Analyzer{
	Name: "declaredAt",
	Doc:  "declaredAt names each file, and where each Logf of another package is declared",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			pass.Reportf(file.Package, "this is %s", pass.Fset.Position(file.Package).Filename)
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				callee := typeutil.Callee(pass.TypesInfo, call)
				if callee != nil && callee.Name() == "Logf" && callee.Pkg() != pass.Pkg {
					pass.Reportf(call.Pos(), "Logf is declared at %s", pass.Fset.Position(callee.Pos()))
				}
				return true
			})
		}
		return nil, nil
	},
}

// The rules' fact reads join the load's reads of the package: printf's port on user asks base's fact of Logf, which
// is there, and of Plain, which is not, and both are recorded, the miss as not found.
func TestARulesFactReadsOfAnotherPackageAreRecordedHitAndMiss(test *testing.T) {
	test.Parallel()

	loaded, byPath := loadedBuildReads(test)
	printfRule := rule.Rule{
		Name:         rule.Namespace + "test-printf",
		Origin:       rule.OriginVet,
		UpstreamName: "printf",
		Analyzer:     printf.Analyzer,
	}
	result, runError := Run(loaded.Packages, []rule.Rule{printfRule}, Options{Reads: loaded.Reads})
	if runError != nil {
		test.Fatal(runError)
	}
	// The type-read walks run past Run, so the reads are whole only once they end.
	result.ReadWalks.Wait()
	if len(result.Crashes) > 0 || len(result.Findings) > 0 {
		test.Fatalf("want a clean run, got crashes %+v and findings %+v", result.Crashes, result.Findings)
	}
	reads := result.Reads[byPath["example.com/reads/user"]]
	if reads == nil {
		test.Fatal("the result holds no reads of user")
	}
	if never := reads.Never(); never != "" {
		test.Fatalf("user is marked never: %s", never)
	}
	var got []string
	for _, fact := range reads.Facts() {
		got = append(got, factLine(fact))
	}
	want := []string{
		"example.com/reads/base.Logf *printf.isWrapper found",
		"example.com/reads/base.Plain *printf.isWrapper missing",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		test.Fatalf("want user's fact reads\n%q\ngot\n%q", want, got)
	}
	if found := reads.Facts()[0]; found.Value == nil {
		test.Fatal("the fact found was recorded without its value")
	}
}

// factLine is one fact read as its object's package and name, its type and whether it was found.
func factLine(fact build_reads.Fact) string {
	outcome := "missing"
	if fact.Found {
		outcome = "found"
	}
	return fmt.Sprintf("%s.%s %s %s", fact.Package.Path(), fact.Object.Name(), fact.FactType, outcome)
}

// A finding whose message names a file of another package can change with a line there, which no read records, so
// its package can never be restored; one naming the package's own file leaves it restorable.
func TestAFindingNamingAnotherPackagesFileMarksItsPackageNever(test *testing.T) {
	test.Parallel()

	loaded, byPath := loadedBuildReads(test)
	result, runError := Run(loaded.Packages, []rule.Rule{houseRule(declaredAtAnalyzer)},
		Options{Reads: loaded.Reads})
	if runError != nil {
		test.Fatal(runError)
	}
	// The type-read walks run past Run, so the reads are whole only once they end.
	result.ReadWalks.Wait()
	want := "a finding of " + rule.Namespace + "test-declaredAt names a file of another package"
	if never := result.Reads[byPath["example.com/reads/user"]].Never(); never != want {
		test.Fatalf("want user marked never\n%s\ngot\n%s", want, never)
	}
	if never := result.Reads[byPath["example.com/reads/base"]].Never(); never != "" {
		test.Fatalf("base, whose one finding names its own file, is marked never: %s", never)
	}
}
