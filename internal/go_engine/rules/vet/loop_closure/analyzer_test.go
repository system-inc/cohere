package loop_closure

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// legacyPackages are x/tools' fixtures whose wants hold only below go1.22, and the parity package, loaded together
// in a module at go1.21.
var legacyPackages = []string{"a", "subtests", "typeparams", "parity"}

// x/tools' own fixtures for loopclosure, copied unchanged into testdata/src. Upstream's test runs one,
// versions/go22.txtar, extracted with its internal testfiles, which this module cannot import, so the archive is
// extracted with the public txtar package instead, and run as upstream runs it, with no flags. Its twin go18.txtar,
// which upstream keeps and no longer runs, is run the same way. The legacy fixtures a, subtests and typeparams hold
// wants that are right only below go1.22, and outside a module their files have no version, which is newer than any,
// so upstream runs none of them; here they run in a module at go1.21, where every want holds. The parity package's
// wants, ours, run there too, so what the parity test compares is what each line says. Each load holds the port to
// every want the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"go18.txtar", "go22.txtar"} {
		test.Run(archive, func(subtest *testing.T) {
			subtest.Parallel()
			parity.RequireUpstreamWants(subtest, Analyzer, extractedArchive(subtest, archive),
				"golang.org/fake/versions")
		})
	}
	test.Run("legacy at go1.21", func(subtest *testing.T) {
		subtest.Parallel()
		parity.RequireUpstreamWants(subtest, Analyzer, legacyModule(subtest), legacyPatterns()...)
	})
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field. Each load judges the files
// at the versions it gives them: the two archives' modules, the legacy fixtures and parity in a module at go1.21, and
// the same packages outside a module, where only a file whose build line names a version is judged. The harness reads
// each load's environment off its directory, a module where it holds a go.mod, with its go.work where it holds one,
// and a tree laid out by import path where it holds neither, so each load is one call, logging its counts under its
// own subtest.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"go18.txtar", "go22.txtar"} {
		test.Run(archive, func(subtest *testing.T) {
			subtest.Parallel()
			parity.RequireIdentical(subtest, loopclosure.Analyzer, Analyzer, extractedArchive(subtest, archive),
				"golang.org/fake/versions")
		})
	}
	test.Run("legacy at go1.21", func(subtest *testing.T) {
		subtest.Parallel()
		parity.RequireIdentical(subtest, loopclosure.Analyzer, Analyzer, legacyModule(subtest), legacyPatterns()...)
	})
	test.Run("outside a module", func(subtest *testing.T) {
		subtest.Parallel()
		parity.RequireIdentical(subtest, loopclosure.Analyzer, Analyzer, analysistest.TestData(), legacyPackages...)
	})
}

// bootstrapSource is one loop whose goroutine captures its variable, in a file whose build line holds it to go1.21.
const bootstrapSource = `//go:build go1.21

package heap

func spawn(values []int) {
	for _, value := range values {
		go func() {
			print(value)
		}()
	}
}
`

// localGroupSource holds a Go of a package-level Group, in a package at errgroup's path, and a Go of a Group declared
// inside a function, which is not the package's. A method of a type declared inside a function can only be an
// interface's, so this is the one shape that reaches the package-level check.
const localGroupSource = `//go:build go1.21

package errgroup

type Group interface{ Go(func() error) }

func shapes(values []int, group Group) {
	for _, value := range values {
		group.Go(func() error {
			print(value)
			return nil
		})
	}
	type Group interface{ Go(func() error) }
	var local Group
	for _, value := range values {
		local.Go(func() error {
			print(value)
			return nil
		})
	}
}
`

// What no fixture package can reach, the judgment of a package by its import path, is run over a package built by
// hand from one type-checked file, which the harness's Analyze runs the original and the port over in one checker. A
// bootstrap package of the standard library is held to the bootstrap toolchain's go1.24 whatever its files say,
// except its tests, a file the type checker recorded no version for is judged, and errgroup's Group is the
// package-level one. go list resolves each of these paths to the standard library or refuses it, so only a package
// built by hand can name them, and the harness's loaders, which go through go list, cannot. Each case's count is the
// original's, checked so a case cannot fall silent, and the port must find exactly what the original finds, each the
// same in every field, as the harness's Difference compares them.
func TestPortJudgesAPackageByItsPathAsTheOriginalDoes(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name        string
		packagePath string
		fileName    string
		source      string
		recorded    bool
		found       int
	}{
		{"a bootstrap package's file", "container/heap", "heap.go", bootstrapSource, true, 0},
		{"a bootstrap package's test", "container/heap", "heap_test.go", bootstrapSource, true, 1},
		{"another package's file", "fixtures/heap", "heap.go", bootstrapSource, true, 1},
		{"a file with no recorded version", "container/heap", "heap.go", bootstrapSource, false, 1},
		{"errgroup's own and a local Group", "golang.org/x/sync/errgroup", "local.go", localGroupSource, true, 1},
	}
	for _, judged := range cases {
		test.Run(judged.name, func(subtest *testing.T) {
			subtest.Parallel()
			checked := checkedByHand(subtest, judged.packagePath, judged.fileName, judged.source)
			if !judged.recorded {
				delete(checked.information.FileVersions, checked.file)
			}
			outcomes, analyzeError := parity.Analyze(
				[]parity.Pair{{Name: passName, Original: loopclosure.Analyzer, Port: Analyzer}},
				[]*packages.Package{checked.asPackage()},
			)
			if analyzeError != nil {
				subtest.Fatal(analyzeError)
			}
			outcome := outcomes[0]
			for _, failure := range slices.Concat(outcome.Original.Failures, outcome.Port.Failures) {
				subtest.Fatalf("running on %s: %s", failure.Package, failure.Error)
			}
			original, port := outcome.Original.Findings, outcome.Port.Findings
			subtest.Logf("the original found %d, the port %d", len(original), len(port))
			if len(original) != judged.found {
				subtest.Fatalf("the original found %d, not the %d the case is written for: %v", len(original),
					judged.found, original)
			}
			for _, missing := range parity.Difference(original, port) {
				subtest.Errorf("the original found %s, and the port did not", missing)
			}
			for _, extra := range parity.Difference(port, original) {
				subtest.Errorf("the port found %s, and the original did not", extra)
			}
		})
	}
}

// errgroupWithVariadicGoSource is a package at errgroup's path whose Group's Go takes any number of functions, called
// with none as a loop's last statement.
const errgroupWithVariadicGoSource = `//go:build go1.21

package errgroup

type Group struct{}

func (*Group) Go(functions ...func() error) {}

func launch(values []int, group *Group) {
	for _, value := range values {
		print(value)
		group.Go()
	}
}
`

// testingWithBareRunSource is a package at testing's path whose T's Run takes a function of no parameters, called
// with a literal in a loop's body.
const testingWithBareRunSource = `//go:build go1.21

package testing

type T struct{}

func (*T) Run(name string, function func()) bool { return true }

func runs(values []int, test *T) {
	for _, value := range values {
		test.Run("", func() {
			print(value)
		})
	}
}
`

// The original indexes a call's first argument and a literal's first parameter where a package at errgroup's or
// testing's path, but not the real one, may have neither, and panics there, while the port reads nothing. A Go given
// no function launches nothing, and a Run's literal with no parameter has no *testing.T to call Parallel on, so
// nothing in either can outlive its iteration and the port, finding nothing, is right. A panic in the checker takes the
// whole test binary down, so the original runs on a hand-built pass under a recover, which pins its crash, and the
// port runs on another over the same file. The harness has no form for a crash, and its differences records pair
// findings, where here the original has none, so this tier keeps its own passes.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name        string
		packagePath string
		source      string
	}{
		{"errgroup's Go with no argument", "golang.org/x/sync/errgroup", errgroupWithVariadicGoSource},
		{"testing's Run with a literal of no parameter", "testing", testingWithBareRunSource},
	}
	for _, judged := range cases {
		test.Run(judged.name, func(subtest *testing.T) {
			subtest.Parallel()
			checked := checkedByHand(subtest, judged.packagePath, "crash.go", judged.source)
			crashed := func() (recovered any) {
				defer func() {
					recovered = recover()
				}()
				runByHand(subtest, loopclosure.Analyzer, checked)
				return nil
			}()
			if crashed == nil {
				subtest.Error("the original did not crash, so the case no longer pins what it was written for")
			}
			subtest.Logf("the original crashed: %v", crashed)
			if port := runByHand(subtest, Analyzer, checked); len(port) > 0 {
				subtest.Errorf("the port found %v, and nothing here can outlive its iteration", port)
			}
		})
	}
}

// handChecked is one file type-checked by hand, as a package of its own.
type handChecked struct {
	fileSet      *token.FileSet
	file         *ast.File
	typesPackage *types.Package
	information  *types.Info
}

// checkedByHand parses source as fileName and type-checks it as the package at packagePath, at go1.27 with the file's
// own build line free to lower it.
func checkedByHand(test *testing.T, packagePath string, fileName string, source string) handChecked {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, fileName, source, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	configuration := types.Config{GoVersion: "go1.27"}
	checked, checkError := configuration.Check(packagePath, fileSet, []*ast.File{file}, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return handChecked{fileSet: fileSet, file: file, typesPackage: checked, information: information}
}

// asPackage is checked as the checker takes a package: its one file, its types and their information, at the sizes
// runByHand gives a pass, with nothing imported, since no source here imports anything.
func (checked handChecked) asPackage() *packages.Package {
	fileName := checked.fileSet.Position(checked.file.Package).Filename
	return &packages.Package{
		ID:              checked.typesPackage.Path(),
		Name:            checked.typesPackage.Name(),
		PkgPath:         checked.typesPackage.Path(),
		GoFiles:         []string{fileName},
		CompiledGoFiles: []string{fileName},
		Imports:         map[string]*packages.Package{},
		Fset:            checked.fileSet,
		Syntax:          []*ast.File{checked.file},
		Types:           checked.typesPackage,
		TypesInfo:       checked.information,
		TypesSizes:      types.SizesFor("gc", "arm64"),
	}
}

// runByHand runs analyzer's Run on a pass over the file checked holds, outside any checker, so a crash in it can be
// recovered, and returns what it reports. Everything
// analyzer requires runs first over the same file, as a driver runs it: the original's inspector, and the port's
// syntax dispatcher with what that walk reads.
func runByHand(test *testing.T, analyzer *analysis.Analyzer, checked handChecked) []analysis.Diagnostic {
	test.Helper()
	found := []analysis.Diagnostic{}
	results := map[*analysis.Analyzer]any{}
	var run func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic))
	run = func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic)) {
		resultOf := map[*analysis.Analyzer]any{}
		for _, required := range running.Requires {
			if _, isRun := results[required]; !isRun {
				run(required, func(analysis.Diagnostic) {
					// A requirement's findings are not the analyzer's, and none of these makes any.
				})
			}
			resultOf[required] = results[required]
		}
		pass := &analysis.Pass{
			Analyzer:   running,
			Fset:       checked.fileSet,
			Files:      []*ast.File{checked.file},
			Pkg:        checked.typesPackage,
			TypesInfo:  checked.information,
			TypesSizes: types.SizesFor("gc", "arm64"),
			ResultOf:   resultOf,
			Report:     report,
		}
		result, runError := running.Run(pass)
		if runError != nil {
			test.Fatalf("running %s: %v", running.Name, runError)
		}
		results[running] = result
	}
	run(analyzer, func(diagnostic analysis.Diagnostic) {
		found = append(found, diagnostic)
	})
	return found
}

// legacyPatterns are legacyPackages' import paths in legacyModule's module.
func legacyPatterns() []string {
	patterns := []string{}
	for _, name := range legacyPackages {
		patterns = append(patterns, "fixtures/"+name)
	}
	return patterns
}

// extractedArchive is the directory x/tools' archive testdata/src/versions/<name> is extracted into, as its internal
// testfiles.ExtractTxtarFileToTmp extracts one.
func extractedArchive(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "src", "versions", name))
	if parseError != nil {
		test.Fatal(parseError)
	}
	files, filesError := txtar.FS(archive)
	if filesError != nil {
		test.Fatal(filesError)
	}
	directory := test.TempDir()
	if copyError := os.CopyFS(directory, files); copyError != nil {
		test.Fatal(copyError)
	}
	return directory
}

// legacyModule is a workspace holding legacyPackages, copied from testdata/src, in a module named fixtures at
// go1.21, and x/tools' stand-in for errgroup in a module of its own, which they import.
func legacyModule(test *testing.T) string {
	test.Helper()
	source := filepath.Join(analysistest.TestData(), "src")
	directory := test.TempDir()
	errgroupModule := filepath.Join("golang.org", "x", "sync")
	for _, name := range append(slices.Clone(legacyPackages), errgroupModule) {
		copied := os.DirFS(filepath.Join(source, name))
		if copyError := os.CopyFS(filepath.Join(directory, name), copied); copyError != nil {
			test.Fatal(copyError)
		}
	}
	written := map[string]string{
		"go.mod":                                "module fixtures\n\ngo 1.21\n",
		filepath.Join(errgroupModule, "go.mod"): "module golang.org/x/sync\n\ngo 1.21\n",
		"go.work":                               "go 1.21\n\nuse (\n\t.\n\t./golang.org/x/sync\n)\n",
	}
	for name, content := range written {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return directory
}
