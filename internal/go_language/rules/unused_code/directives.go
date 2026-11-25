package unused_code

import (
	"go/ast"
	"go/token"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/lint"
)

// directivesAnalyzer is every //lint: directive in a package, the port's own copy of staticcheck's directives
// analyzer (analysis/facts/directives), which U1000 reads to learn what a //lint:ignore quiets. It returns what the
// original returns, run as the original runs, despite type errors, but it reads the package through parseDirectives,
// which skips a file with no directive. Named apart from the original's `directives`, since the tests run both.
var directivesAnalyzer = &analysis.Analyzer{
	Name:             "lint_directives",
	Doc:              "extracts staticcheck's //lint: directives, skipping a file with none",
	Run:              runDirectives,
	RunDespiteErrors: true,
	ResultType:       reflect.TypeFor[[]lint.Directive](),
}

func runDirectives(pass *analysis.Pass) (any, error) {
	return parseDirectives(pass.Files, pass.Fset), nil
}

// parseDirectives is staticcheck v0.8.1's lint.ParseDirectives, but for a file none of whose comments starts
// `//lint:`, which it skips before building the file's comment map. That is the test the release puts to each comment
// the map holds, so such a file yields no directive either way, and a file with one builds its map as released, each
// directive keeping its Node. Almost no file has one, 14 in cohere's own 5,332 Go files, and the map was most of the
// analyzer's CPU, about 0.35 CPU-s on a cold run at cohere's root (#tcbjtmj). The release's own `OPT(dh)` note above
// the map asks whether skipping it is worth doing.
func parseDirectives(files []*ast.File, fileSet *token.FileSet) []lint.Directive {
	var directives []lint.Directive
	for _, file := range files {
		if !hasDirectiveComment(file) {
			continue
		}
		commentMap := ast.NewCommentMap(fileSet, file, file.Comments)
		for node, groups := range commentMap {
			for _, group := range groups {
				for _, comment := range group.List {
					if !strings.HasPrefix(comment.Text, "//lint:") {
						continue
					}
					// The release splits on single spaces, so two spaces in a row make an empty argument.
					fields := strings.Split(strings.TrimPrefix(comment.Text, "//lint:"), " ")
					directives = append(directives, lint.Directive{
						Command:   fields[0],
						Arguments: fields[1:],
						Directive: comment,
						Node:      node,
					})
				}
			}
		}
	}
	return directives
}

// hasDirectiveComment is whether any comment of file starts `//lint:`.
func hasDirectiveComment(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//lint:") {
				return true
			}
		}
	}
	return false
}
