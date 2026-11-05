package rule

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEverySkipSaysWhichKindItIs reads every rule's source: each Skip gives its reason, and each
// SkipCovered names a declared cover and gives its reason, as a string literal a reader can find. A skip
// with no reason is a rule that checked nothing and said nothing, which is the failure Skip exists to end.
func TestEverySkipSaysWhichKindItIs(test *testing.T) {
	test.Parallel()
	covers := map[string]bool{"CoverTypeCheck": true}
	calls := 0
	walkError := filepath.WalkDir("../rules", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector || (selector.Sel.Name != "Skip" && selector.Sel.Name != "SkipCovered") {
				return true
			}
			calls++
			reasonIndex := 0
			if selector.Sel.Name == "SkipCovered" {
				reasonIndex = 1
				cover, isCover := call.Args[0].(*ast.SelectorExpr)
				if !isCover || !covers[cover.Sel.Name] {
					test.Errorf("%s: SkipCovered names a cover that is not one of rule's declared covers", path)
				}
			}
			if len(call.Args) <= reasonIndex {
				test.Errorf("%s: %s gives no reason", path, selector.Sel.Name)
				return true
			}
			literal, isLiteral := call.Args[reasonIndex].(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING {
				test.Errorf("%s: %s's reason is not a string literal", path, selector.Sel.Name)
				return true
			}
			reason, unquoteError := strconv.Unquote(literal.Value)
			if unquoteError != nil {
				test.Errorf("%s: %s's reason does not unquote: %v", path, selector.Sel.Name, unquoteError)
				return true
			}
			if strings.TrimSpace(reason) == "" {
				test.Errorf("%s: %s's reason is empty", path, selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	// The walk finding no skip at all would pass every assertion above, so it must find the ones that exist.
	if calls == 0 {
		test.Fatal("found no Skip or SkipCovered call under ../rules, so nothing was checked")
	}
}
