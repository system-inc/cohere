package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/positions"
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

// The registry never hands a type-aware rule a pass with no type information (#f5g7ym6). It makes one two ways, both
// for a rule whose findings on a file are the file's bytes' alone: a listener marked SyntaxOnly hears every build
// through a syntax-only context (rule.NewSyntaxOnlyContext), and a syntax-only walk runs through rule.SyntaxOnlyRun,
// each holding no TypesInfo. Every other pass holds its package's, which the load sets on every package it checks, an
// empty one on a package checked for its types only. A type-aware rule marked a syntax-only walk is refused as the
// package initializes (rule.ValidateSyntaxOnlyWalk), and this holds the listeners to the same: a rule that guards a
// missing TypesInfo, as consistency-no-any does for a binary's typeless parity walk, would judge nothing there and say
// nothing, and a real typeless pass would read as a clean one. A registry with no listener marked SyntaxOnly, or no
// type-aware rule, proves nothing and fails.
func TestTheRegistryHandsNoTypeAwareRuleATypelessPass(test *testing.T) {
	test.Parallel()
	syntaxOnlyListeners, typeAware := 0, 0
	for _, registered := range All() {
		listensSyntaxOnly := registered.Listener != nil && registered.Listener.SyntaxOnly
		if listensSyntaxOnly {
			syntaxOnlyListeners++
		}
		if !registered.TypeAware {
			continue
		}
		typeAware++
		if listensSyntaxOnly {
			test.Errorf("%s is type aware and its listener is marked SyntaxOnly, so every build hands it a context "+
				"with no type information", registered.Name)
		}
	}
	if syntaxOnlyListeners == 0 || typeAware == 0 {
		test.Fatalf("the registry holds %d listeners marked SyntaxOnly and %d type-aware rules, too few to prove "+
			"anything", syntaxOnlyListeners, typeAware)
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

// Each rule syntaxOnlyWalks names is run by the registry through rule.SyntaxOnlyRun (#y6q1x2j): second.go holds what
// every marked rule reports, a build line with a malformed Go version, a go:debug directive outside package main, a
// long line of shouting with a deferred task in it, and more lines than a file may hold, so each rule's registered
// analyzer, under an inheritance that says the rule inherits second.go, reports the host's finding there and nothing of
// its own, which only the wrap can do, and inheriting nothing it reports its own there, which shows second.go provokes
// it.
func TestTheRegistrysSyntaxOnlyWalksAreWrapped(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"first.go": "package walked\n",
		"second.go": "//go:build go1.x\n//go:debug panicnil=1\n\npackage walked\n\n// A LOUD comment, TODO " +
			strings.Repeat("that runs on ", 10) + "past the limit.\n" + strings.Repeat("\n", 2001),
	}
	registered := All()
	for name := range syntaxOnlyWalks {
		at := slices.IndexFunc(registered, func(each rule.Rule) bool { return each.Name == name })
		if at < 0 {
			test.Errorf("%s is marked a syntax-only walk and is not registered", name)
			continue
		}
		inheriting := walkedSecond(test, registered[at].Analyzer, sources, inheritsSecond{})
		expected := []string{"second.go:1:1 inherited the host's finding"}
		if !slices.Equal(inheriting, expected) {
			test.Errorf("%s reported %q, want only %q, so the registry does not run it through rule.SyntaxOnlyRun",
				name, inheriting, expected)
		}
		own := walkedSecond(test, registered[at].Analyzer, sources, nil)
		if !slices.ContainsFunc(own, func(finding string) bool { return strings.HasPrefix(finding, "second.go:") }) {
			test.Errorf("%s reported %q inheriting nothing, want a finding of its own on second.go", name, own)
		}
	}
}

// walkedSecond is what analyzer reports, each finding as its position, category and message, over first.go and
// second.go parsed from sources, under inheritance, the positions table answered as the checker answers it.
//
//cohere:ignore consistency-no-any a pass's ResultOf holds each result as any, and inheritance is one
func walkedSecond(test *testing.T, analyzer *analysis.Analyzer, sources map[string]string, inheritance any) []string {
	test.Helper()
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
	pass := &analysis.Pass{Analyzer: analyzer, Fset: fileSet, Files: files,
		ResultOf: map[*analysis.Analyzer]any{rule.Inherited: inheritance,
			positions.Analyzer: positions.New(fileSet, files)},
		ReadFile: func(filename string) ([]byte, error) { return []byte(sources[filename]), nil },
		Report: func(diagnostic analysis.Diagnostic) {
			position := fileSet.PositionFor(diagnostic.Pos, false)
			reported = append(reported, position.String()+" "+diagnostic.Category+" "+diagnostic.Message)
		}}
	if _, runError := analyzer.Run(pass); runError != nil {
		test.Fatal(runError)
	}
	return reported
}

// A walk marked syntax-only is handed a pass with no type information, no package and no sizes, whether or not it
// inherits a file, so one that reads any of them panics in its own fixtures rather than pass silently while an extra
// build hands on the host's findings (#y6q1x2j).
func TestAMarkedSyntaxOnlyWalkThatReadsTypesPanics(test *testing.T) {
	test.Parallel()
	reads := map[string]func(*analysis.Pass, *ast.File){
		"the type information": func(pass *analysis.Pass, file *ast.File) { _ = pass.TypesInfo.TypeOf(file.Name) },
		"the package":          func(pass *analysis.Pass, _ *ast.File) { _ = pass.Pkg.Path() },
		"the sizes": func(pass *analysis.Pass, _ *ast.File) {
			_ = pass.TypesSizes.Alignof(types.Typ[types.Int])
		},
	}
	inheritances := map[string]any{"inheriting second.go": inheritsSecond{}, "inheriting nothing": nil}
	for read, reading := range reads {
		for inheriting, inheritance := range inheritances {
			run := rule.SyntaxOnlyRun(rule.Namespace+"consistency-test-typed", func(pass *analysis.Pass) (any, error) {
				for _, file := range pass.Files {
					reading(pass, file)
				}
				return nil, nil
			})
			fileSet := token.NewFileSet()
			files := []*ast.File{}
			for _, filename := range []string{"first.go", "second.go"} {
				file, parseError := parser.ParseFile(fileSet, filename, "package typed\n", 0)
				if parseError != nil {
					test.Fatal(parseError)
				}
				files = append(files, file)
			}
			pass := &analysis.Pass{Fset: fileSet, Files: files, Pkg: types.NewPackage("typed", "typed"),
				TypesInfo: &types.Info{}, TypesSizes: types.SizesFor("gc", "amd64"),
				ResultOf: map[*analysis.Analyzer]any{rule.Inherited: inheritance},
				Report: func(analysis.Diagnostic) {
					// Nothing reports: the walk panics on its first file, and a walk that did not is the failure.
				}}
			// Ignored: the walk returns no error, and whether it panics is all this asks.
			if !panics(func() { _, _ = run(pass) }) {
				test.Errorf("a marked walk %s read %s and did not panic", inheriting, read)
			}
		}
	}
}

// panics is whether calling call panics.
func panics(call func()) (panicked bool) {
	defer func() {
		panicked = recover() != nil
	}()
	call()
	return false
}
