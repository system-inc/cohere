package loop_closure

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
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
// wants, ours, run there too, so what the parity test compares is what each line says. Every `// want` is met, and
// analysistest fails on a finding no want expects as much as on a want nothing found.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"go18.txtar", "go22.txtar"} {
		test.Run(archive, func(subtest *testing.T) {
			subtest.Parallel()
			directory := extractedArchive(subtest, archive)
			analysistest.Run(errorsOnly{subtest}, directory, Analyzer, "golang.org/fake/versions")
		})
	}
	test.Run("legacy at go1.21", func(subtest *testing.T) {
		subtest.Parallel()
		analysistest.Run(errorsOnly{subtest}, legacyModule(subtest), Analyzer, legacyPatterns()...)
	})
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ, and their findings must be the same
// multiset of file, line, column, end and message. Each load judges the files at the versions it gives them: the two
// archives' modules, the legacy fixtures and parity in a module at go1.21, and the same packages outside a module,
// where only a file whose build line names a version is judged. Each side's count is logged per load, since two
// empty sides would match and prove nothing, and the test fails if the original finds nothing in a load.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	gopath := analysistest.TestData()
	legacy := legacyModule(test)
	loads := []fixtureLoad{
		{
			name:        "go18.txtar",
			directory:   extractedArchive(test, "go18.txtar"),
			environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"},
			patterns:    []string{"golang.org/fake/versions"},
		},
		{
			name:        "go22.txtar",
			directory:   extractedArchive(test, "go22.txtar"),
			environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"},
			patterns:    []string{"golang.org/fake/versions"},
		},
		{
			name:        "legacy at go1.21",
			directory:   legacy,
			environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + filepath.Join(legacy, "go.work")},
			patterns:    legacyPatterns(),
		},
		{
			name:        "outside a module",
			directory:   gopath,
			environment: []string{"GOPATH=" + gopath, "GO111MODULE=off", "GOWORK=off"},
			patterns:    legacyPackages,
		},
	}
	for _, fixtures := range loads {
		original, port := findingsOf(test, fixtures)
		test.Logf("%s: the original found %d, the port %d", fixtures.name, len(original), len(port))
		if len(original) == 0 {
			test.Errorf("%s: the original found nothing, so the comparison proves nothing", fixtures.name)
		}
		for _, missing := range difference(original, port) {
			test.Errorf("%s: the original found %s, and the port did not", fixtures.name, described(missing))
		}
		for _, extra := range difference(port, original) {
			test.Errorf("%s: the port found %s, and the original did not", fixtures.name, described(extra))
		}
	}
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

// What no fixture package can reach, the judgment of a package by its import path, is run on passes built by hand,
// one for the original and one for the port, over the same type-checked file. A bootstrap package of the standard
// library is held to the bootstrap toolchain's go1.24 whatever its files say, except its tests, a file the type
// checker recorded no version for is judged, and errgroup's Group is the package-level one. go list resolves each of
// these paths to the standard library or refuses it, so only a hand-built pass can name them. Each case's count is
// the original's, checked so a case cannot fall silent, and the port must find exactly what the original finds.
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
			original := runByHand(subtest, loopclosure.Analyzer, checked)
			port := runByHand(subtest, Analyzer, checked)
			subtest.Logf("the original found %d, the port %d", len(original), len(port))
			if len(original) != judged.found {
				subtest.Fatalf("the original found %d, not the %d the case is written for: %v", len(original),
					judged.found, original)
			}
			for _, missing := range difference(original, port) {
				subtest.Errorf("the original found %s, and the port did not", described(missing))
			}
			for _, extra := range difference(port, original) {
				subtest.Errorf("the port found %s, and the original did not", described(extra))
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
// port runs on another over the same file.
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

// runByHand runs analyzer's Run on a pass over the file checked holds, with the inspector it requires, and returns
// what it reports.
func runByHand(test *testing.T, analyzer *analysis.Analyzer, checked handChecked) []finding {
	test.Helper()
	found := []finding{}
	pass := &analysis.Pass{
		Analyzer:   analyzer,
		Fset:       checked.fileSet,
		Files:      []*ast.File{checked.file},
		Pkg:        checked.typesPackage,
		TypesInfo:  checked.information,
		TypesSizes: types.SizesFor("gc", "arm64"),
		ResultOf:   map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New([]*ast.File{checked.file})},
		Report: func(diagnostic analysis.Diagnostic) {
			found = append(found, findingOf(checked.fileSet, diagnostic))
		},
	}
	if _, runError := analyzer.Run(pass); runError != nil {
		test.Fatalf("running %s: %v", analyzer.Name, runError)
	}
	return found
}

// fixtureLoad is one load of fixture packages: from directory, under environment, the packages patterns name.
type fixtureLoad struct {
	name        string
	directory   string
	environment []string
	patterns    []string
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

// finding is one finding as the differential compares it.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it, with where it ends.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingOf is diagnostic as the differential compares it.
func findingOf(fileSet *token.FileSet, diagnostic analysis.Diagnostic) finding {
	position := fileSet.Position(diagnostic.Pos)
	end := fileSet.Position(diagnostic.End)
	return finding{
		file:      filepath.Base(position.Filename),
		line:      position.Line,
		column:    position.Column,
		endLine:   end.Line,
		endColumn: end.Column,
		message:   diagnostic.Message,
	}
}

// findingsOf loads the fixture packages fixtures names, runs x/tools' loopclosure and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, fixtures fixtureLoad) (original []finding, port []finding) {
	test.Helper()
	loaded, loadError := load.Packages(fixtures.directory, fixtures.patterns, fixtures.environment, nil)
	if loadError != nil {
		test.Fatalf("%s: loading %v: %v", fixtures.name, fixtures.patterns, loadError)
	}
	// The checker passes over a root that does not type-check without a word, so a fixture with one type error would
	// compare nothing and read green. Every root must load clean, and there must be one.
	if len(loaded.Roots) == 0 {
		test.Fatalf("%s: loading %v found no package, so the comparison would prove nothing", fixtures.name,
			fixtures.patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("%s: fixture package %s does not load clean, so its findings would go uncounted: %v",
				fixtures.name, root.ID, root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{loopclosure.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("%s: analyzing %v: %v", fixtures.name, fixtures.patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("%s: running %s: %v", fixtures.name, action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(action.Package.Fset, diagnostic)
			switch action.Analyzer {
			case loopclosure.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
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
