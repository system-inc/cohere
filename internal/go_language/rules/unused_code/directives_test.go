package unused_code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/analysis/lint"
)

// directiveCase is one file parseDirectives reads: its source, and how many directives it holds. A file with none is
// one the fast path skips without building its comment map.
type directiveCase struct {
	name       string
	source     string
	directives int
}

// directiveCases are files with a directive where the release finds one, in a comment group's later line, in a later
// group than the first, above the package clause, and files the fast path skips: no comment at all, comments none of
// which is a directive, and `//lint:` inside a block comment and inside a string, neither of which the release's
// comment map ever read as a directive.
var directiveCases = []directiveCase{
	{
		name: "a directive on a doc comment's second line, in the file's second group",
		source: "package kept\n\n// Package comment, no directive.\n\nvar used = 1\n\n" +
			"// kept is held on purpose.\n//lint:ignore U1000 kept for the test\nfunc kept() {}\n",
		directives: 1,
	},
	{
		name:       "a file ignore above the package clause",
		source:     "//lint:file-ignore U1000 everything here is used\n\npackage ignored\n\nfunc first() {}\n",
		directives: 1,
	},
	{
		name: "two directives, one trailing a declaration",
		source: "package two\n\n//lint:ignore U1000 first\nfunc first() {}\n\n" +
			"var second = 2 //lint:ignore U1000 second\n",
		directives: 2,
	},
	{
		name:       "no comment at all",
		source:     "package bare\n\nfunc bare() {}\n",
		directives: 0,
	},
	{
		name:       "comments, none a directive",
		source:     "// Package plain says lint: in passing.\npackage plain\n\n// plain is plain.\nfunc plain() {} // lint:ignore\n",
		directives: 0,
	},
	{
		name: "a directive inside a block comment and a string",
		source: "package hidden\n\n/* //lint:ignore U1000 inside a block */\nfunc hidden() {}\n\n" +
			"/*//lint:ignore U1000 against the opener*/\nvar text = \"//lint:ignore U1000 in a string\"\n",
		directives: 0,
	},
}

// TestParseDirectivesReadsAsTheReleaseAndSkipsFilesWithoutOne holds parseDirectives, the port's own reading of the
// //lint: directives (#tcbjtmj), to staticcheck v0.8.1's lint.ParseDirectives, which builds a comment map for every
// file. Each file's directives must be the release's, read through ast.NewCommentMap as the release reads them, each
// with the same comment and the same Node, and a file with none must allocate nothing, which only the fast path does,
// since a comment map for any comment allocates.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestParseDirectivesReadsAsTheReleaseAndSkipsFilesWithoutOne(test *testing.T) {
	for _, directiveCase := range directiveCases {
		// Not parallel: each case counts the process's allocations, as its parent does.
		test.Run(directiveCase.name, func(test *testing.T) {
			fileSet := token.NewFileSet()
			file, parseError := parser.ParseFile(fileSet, "case.go", directiveCase.source, parser.ParseComments)
			if parseError != nil {
				test.Fatalf("parsing the case: %v", parseError)
			}
			files := []*ast.File{file}
			found := sortedDirectives(parseDirectives(files, fileSet))
			expected := sortedDirectives(directivesThroughCommentMap(files, fileSet))
			if len(expected) != directiveCase.directives {
				test.Fatalf("the release's reading finds %d directives, the case expects %d", len(expected),
					directiveCase.directives)
			}
			if len(found) != len(expected) {
				test.Fatalf("parseDirectives found %d directives, the release's reading %d", len(found),
					len(expected))
			}
			for index, directive := range found {
				releaseDirective := expected[index]
				if directive.Directive != releaseDirective.Directive || directive.Node != releaseDirective.Node ||
					directive.Command != releaseDirective.Command ||
					!slices.Equal(directive.Arguments, releaseDirective.Arguments) {
					test.Errorf("directive %d is %q on %T, the release's is %q on %T", index,
						directive.Directive.Text, directive.Node,
						releaseDirective.Directive.Text, releaseDirective.Node)
				}
			}
			if directiveCase.directives == 0 && len(file.Comments) > 0 {
				allocations := testing.AllocsPerRun(10, func() { parseDirectives(files, fileSet) })
				if allocations != 0 {
					test.Errorf("parseDirectives allocated %.0f times over a file with no directive: it built the "+
						"comment map the fast path skips", allocations)
				}
			}
		})
	}
}

// directivesThroughCommentMap is lint.ParseDirectives as staticcheck v0.8.1 released it, a comment map built for every
// file and only its `//lint:` comments kept, the oracle parseDirectives is held to.
func directivesThroughCommentMap(files []*ast.File, fileSet *token.FileSet) []lint.Directive {
	var directives []lint.Directive
	for _, file := range files {
		commentMap := ast.NewCommentMap(fileSet, file, file.Comments)
		for node, groups := range commentMap {
			for _, group := range groups {
				for _, comment := range group.List {
					if !strings.HasPrefix(comment.Text, "//lint:") {
						continue
					}
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

// sortedDirectives orders directives by where their comments start, since both readings range over a map.
func sortedDirectives(directives []lint.Directive) []lint.Directive {
	slices.SortFunc(directives, func(left lint.Directive, right lint.Directive) int {
		return int(left.Directive.Slash) - int(right.Directive.Slash)
	})
	return directives
}
