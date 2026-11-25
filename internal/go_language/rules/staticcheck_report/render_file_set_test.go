package staticcheck_report

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// renderFileSetSources are the files of one package parsed into one file set, between files of the set that aren't
// the package's, so a set of one file that placed a position wrongly, or a lookup that found a neighbor, would print
// another line break or comment where the shared set prints none.
var renderFileSetSources = []struct {
	name   string
	source string
}{
	{"before.go", "package other\n\nvar Before = 1\n"},
	{"first.go", "// The first file starts with its comment.\npackage sample\n\n" +
		"import (\n\t\"strings\"\n\t\"fmt\"\n)\n\n" +
		"func First(values []int, name string) string {\n\tif len(values) > 0 &&\n\t\tlen(name) > 0 {\n" +
		"\t\treturn fmt.Sprint(values[0], // the first\n\t\t\tstrings.ToUpper(name))\n\t}\n\treturn name\n}\n"},
	{"second.go", "package sample\n\ntype pair struct {\n\tleft  int // the left one\n\tright int\n}\n\n" +
		"func Second(table map[string]pair) int {\n\ttotal := 0\n\tfor _, value := range table {\n" +
		"\t\ttotal += value.left*(value.right-1) +\n\t\t\tvalue.right\n\t}\n\treturn total\n}\n"},
	{"after.go", "package other\n\nvar After = 2\n"},
}

// TestRenderPrintsThroughTheNodesFileAlone holds Render to a file set of its own holding only the file of the node it
// prints, which the shared set's cache of the last file found is never asked about, and to the same text the shared
// set prints, at every node of a package of two files that sit between files of the same set that aren't the
// package's. A node built from two files' syntax, and one that starts nowhere, print through the shared set.
func TestRenderPrintsThroughTheNodesFileAlone(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	parsed := map[string]*ast.File{}
	for _, source := range renderFileSetSources {
		file, parseError := parser.ParseFile(fileSet, source.name, source.source, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", source.name, parseError)
		}
		parsed[source.name] = file
	}
	context := &rule.Context{FileSet: fileSet, Files: []*ast.File{parsed["first.go"], parsed["second.go"]}}
	printedThroughShared := func(node ast.Node) (string, error) {
		printed := &bytes.Buffer{}
		printError := format.Node(printed, fileSet, node)
		return printed.String(), printError
	}
	rendered := 0
	for _, file := range context.Files {
		tokenFile := fileSet.File(file.Pos())
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				return false
			}
			rendered++
			renderingSet := renderingFileSet(context, node)
			if renderingSet == fileSet {
				test.Errorf("%T at %s prints through the shared file set", node, fileSet.Position(node.Pos()))
				return true
			}
			held := []*token.File{}
			renderingSet.Iterate(func(heldFile *token.File) bool {
				held = append(held, heldFile)
				return true
			})
			if len(held) != 1 || held[0] != tokenFile {
				test.Errorf("%T at %s prints through a set of %d files, not its own file alone", node,
					fileSet.Position(node.Pos()), len(held))
			}
			// A node the printer refuses panics in Render, as it does through the shared set; the text of the rest
			// is the shared set's.
			expected, printError := printedThroughShared(node)
			if printError != nil {
				return true
			}
			if got := Render(context, node); got != expected {
				test.Errorf("%T at %s renders %q, the shared set %q", node, fileSet.Position(node.Pos()), got, expected)
			}
			return true
		})
	}
	test.Logf("%d nodes rendered both ways", rendered)

	// The condition of First's if statement, and the first identifier of Second's body.
	var firstCondition ast.Expr
	var secondTotal ast.Expr
	ast.Inspect(parsed["first.go"], func(node ast.Node) bool {
		if binary, isBinary := node.(*ast.BinaryExpr); isBinary && binary.Op == token.LAND && firstCondition == nil {
			firstCondition = binary
		}
		return true
	})
	ast.Inspect(parsed["second.go"], func(node ast.Node) bool {
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier && identifier.Name == "total" &&
			secondTotal == nil {
			secondTotal = identifier
		}
		return true
	})
	if firstCondition == nil || secondTotal == nil {
		test.Fatalf("the sources lost First's condition or Second's total, which the mixed nodes are built from")
	}
	for name, node := range map[string]ast.Node{
		"a node built from two files": &ast.BinaryExpr{
			X: firstCondition, OpPos: firstCondition.End(), Op: token.LAND, Y: secondTotal,
		},
		"a node that starts nowhere": &ast.UnaryExpr{Op: token.NOT, X: firstCondition},
	} {
		if renderingFileSet(context, node) != fileSet {
			test.Errorf("%s prints through a set of its own, not the shared set", name)
		}
	}
}
