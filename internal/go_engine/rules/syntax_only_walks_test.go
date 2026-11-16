package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Every rule syntaxOnlyWalks names is registered, and the mark holds for it (rule.ValidateSyntaxOnlyWalk), so All's
// marking never panics.
func TestTheRegistrysSyntaxOnlyWalksHold(test *testing.T) {
	test.Parallel()
	if !syntaxOnlyWalksMarked {
		test.Fatal("the registry's syntax-only walks were not marked as the package initialized")
	}
	registered := All()
	for name := range syntaxOnlyWalks {
		at := slices.IndexFunc(registered, func(each rule.Rule) bool { return each.Name == name })
		if at < 0 {
			test.Errorf("%s is marked a syntax-only walk and is not registered", name)
			continue
		}
		if validateError := rule.ValidateSyntaxOnlyWalk(registered[at]); validateError != nil {
			test.Error(validateError)
		}
	}
}

// A rule marked a syntax-only walk runs through rule.SyntaxOnlyRun (#y6q1x2j): handed a pass whose inheritance says
// second.go is inherited, its own analyzer walks first.go alone, and the pass reports its finding there and then the
// host's on second.go, placed at second.go's package clause. A mark naming no registered rule, or a dispatched one, is
// refused.
func TestAMarkedSyntaxOnlyWalkIsHandedOnlyTheFilesItDoesNotInherit(test *testing.T) {
	test.Parallel()
	walked := []string{}
	analyzer := &analysis.Analyzer{
		Name: "walking",
		Doc:  "reports each file it walks",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				walked = append(walked, filepath.Base(pass.Fset.File(file.FileStart).Name()))
				pass.Reportf(file.Name.Pos(), "walked")
			}
			return nil, nil
		},
	}
	walking := rule.Rule{Name: rule.Namespace + "consistency-test-walking", Origin: rule.OriginHouse,
		Analyzer: analyzer}
	dispatched := rule.Rule{Name: rule.Namespace + "consistency-test-listening", Origin: rule.OriginHouse,
		Analyzer: &analysis.Analyzer{Name: "listening", Doc: "listens", Run: analyzer.Run},
		Listener: &rule.Listener{FileEnter: func(*rule.Context, *ast.File) {
			// Never called: the mark is refused before anything runs.
		}, FileLocal: true, SyntaxOnly: true}}
	refusals := map[string]map[string]bool{
		"no rule is registered under it": {rule.Namespace + "consistency-test-missing": true},
		"has a listener":                 {dispatched.Name: true},
	}
	for saying, marked := range refusals {
		_, walksError := syntaxOnlyWalksOf([]rule.Rule{walking, dispatched}, marked)
		if walksError == nil || !strings.Contains(walksError.Error(), saying) {
			test.Errorf("marking %v refused with %v, want it to say %q", marked, walksError, saying)
		}
	}
	walks, walksError := syntaxOnlyWalksOf([]rule.Rule{walking, dispatched}, map[string]bool{walking.Name: true})
	if walksError != nil || len(walks) != 1 || walks[0].Analyzer != analyzer {
		test.Fatalf("marking walking gave %d rules and %v, want walking alone", len(walks), walksError)
	}
	// As markedSyntaxOnlyWalks marks each, which only the package's initialization may call.
	analyzer.Run = rule.SyntaxOnlyRun(walking.Name, analyzer.Run)

	fileSet := token.NewFileSet()
	files := []*ast.File{}
	for _, name := range []string{"first.go", "second.go"} {
		file, parseError := parser.ParseFile(fileSet, name, "package walked\n", 0)
		if parseError != nil {
			test.Fatal(parseError)
		}
		files = append(files, file)
	}
	reported := []string{}
	pass := &analysis.Pass{Analyzer: analyzer, Fset: fileSet, Files: files,
		ResultOf: map[*analysis.Analyzer]any{rule.Inherited: inheritsSecond{}},
		Report: func(diagnostic analysis.Diagnostic) {
			position := fileSet.PositionFor(diagnostic.Pos, false)
			reported = append(reported, position.String()+" "+diagnostic.Category+" "+diagnostic.Message)
		}}
	if _, runError := analyzer.Run(pass); runError != nil {
		test.Fatal(runError)
	}
	expected := []string{"first.go:1:9  walked", "second.go:1:1 inherited the host's finding"}
	if !slices.Equal(walked, []string{"first.go"}) || !slices.Equal(reported, expected) {
		test.Errorf("the marked walk walked %q and reported %q, want first.go alone and %q", walked, reported,
			expected)
	}
}

// Each rule syntaxOnlyWalks names is run by the registry through rule.SyntaxOnlyRun (#y6q1x2j): handed second.go, a
// file holding a long line of shouting either rule would report, under an inheritance that says the rule inherits
// second.go, its registered analyzer reports the host's finding there and nothing of its own, which only the wrap can
// do.
func TestTheRegistrysSyntaxOnlyWalksAreWrapped(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"first.go":  "package walked\n",
		"second.go": "package walked\n\n// A LOUD comment " + strings.Repeat("that runs on ", 10) + "past the limit.\n",
	}
	registered := All()
	for name := range syntaxOnlyWalks {
		at := slices.IndexFunc(registered, func(each rule.Rule) bool { return each.Name == name })
		if at < 0 {
			test.Errorf("%s is marked a syntax-only walk and is not registered", name)
			continue
		}
		fileSet := token.NewFileSet()
		files := []*ast.File{}
		for _, filename := range []string{"first.go", "second.go"} {
			file, parseError := parser.ParseFile(fileSet, filename, sources[filename], parser.ParseComments)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, file)
		}
		reported := []string{}
		pass := &analysis.Pass{Analyzer: registered[at].Analyzer, Fset: fileSet, Files: files,
			ResultOf: map[*analysis.Analyzer]any{rule.Inherited: inheritsSecond{}},
			ReadFile: func(filename string) ([]byte, error) { return []byte(sources[filename]), nil },
			Report: func(diagnostic analysis.Diagnostic) {
				position := fileSet.PositionFor(diagnostic.Pos, false)
				reported = append(reported, position.String()+" "+diagnostic.Category+" "+diagnostic.Message)
			}}
		if _, runError := registered[at].Analyzer.Run(pass); runError != nil {
			test.Fatal(runError)
		}
		expected := []string{"second.go:1:1 inherited the host's finding"}
		if !slices.Equal(reported, expected) {
			test.Errorf("%s reported %q, want only %q, so the registry does not run it through rule.SyntaxOnlyRun",
				name, reported, expected)
		}
	}
}
