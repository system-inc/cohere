package unused_code

import (
	"fmt"
	"go/token"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"
)

// Analyzer is U1000, cohere's port of staticcheck's, under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read as they did when the registry ran the original. It reads the package's //lint:
// directives, through directivesAnalyzer, its own copy of staticcheck's that skips a file with none, and staticcheck's
// generated files, as the original does, and reports each unused object as the registry reported the original's: at
// the object's position, worded `<kind> <name> is unused`, U1000 its category. It also returns its verdict on every
// object, used, unused or quiet, which the original returns for staticcheck's runner to report and which the tests
// beside this file hold to the original's expectations.
//
// It is an ordinary analyzer for now. The engine's registry of listeners (#29k62f4) is still being built, and moving
// onto it is a small change once it lands.
var Analyzer = &analysis.Analyzer{
	Name:       "U1000",
	Doc:        "Unused code: staticcheck's U1000, ported as cohere's own.",
	Run:        run,
	Requires:   []*analysis.Analyzer{generated.Analyzer, directivesAnalyzer},
	ResultType: reflect.TypeFor[result](),
}

// category is the category every finding carries, U1000's, as the original's findings carried it.
const category = "U1000"

func run(pass *analysis.Pass) (any, error) {
	packageDirectives, isDirectives := pass.ResultOf[directivesAnalyzer].([]lint.Directive)
	if !isDirectives {
		return nil, fmt.Errorf("reading the //lint: directives: they are %T", pass.ResultOf[directivesAnalyzer])
	}
	generatedFiles, isGenerated := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	if !isGenerated {
		return nil, fmt.Errorf("reading staticcheck's generated files: they are %T", pass.ResultOf[generated.Analyzer])
	}

	graph := newGraph(pass.Fset, pass.Files, pass.Pkg, pass.TypesInfo, packageDirectives, generatedFiles)
	graph.entry()
	verdict := graph.results()

	// Reported as the registry reported the original's, which the tests' originalReport still does: an unused object
	// names its file by the path its own position gives, never one a //line directive gives, so each path maps back to
	// the token file a position needs, and an object in no file of the pass goes unreported.
	tokenFiles := map[string]*token.File{}
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.Pos())
		if tokenFile == nil {
			continue
		}
		tokenFiles[tokenFile.Name()] = tokenFile
	}
	for _, unused := range verdict.unused {
		position := pass.Fset.PositionFor(unused.position, false)
		tokenFile, found := tokenFiles[position.Filename]
		if !found {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:      tokenFile.Pos(position.Offset),
			Category: category,
			Message:  fmt.Sprintf("%s %s is unused", unused.kind, unused.name),
		})
	}
	return verdict, nil
}
