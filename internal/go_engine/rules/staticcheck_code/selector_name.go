package staticcheck_code

import (
	"fmt"
	"go/ast"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// SelectorName is the name of what selector selects, as staticcheck spells it to compare against the names it knows:
// "(net/http.Request).Header" for a field, named on its struct with any pointer taken off, "(*bytes.Buffer).Write"
// for a method, named on the receiver it is selected through, and "net/http.Get" for a member of an imported package,
// its package named by its full path. It is staticcheck's code.SelectorName, from honnef.co/go/tools@v0.8.1
// analysis/code/code.go.
func SelectorName(context *rule.Context, selector *ast.SelectorExpr) string {
	selection := context.TypesInfo.Selections[selector]
	if selection == nil {
		// A selector the type checker recorded no selection of is a qualified identifier, a package's member.
		switch qualifier := selector.X.(type) {
		case *ast.Ident:
			imported, isPackageName := context.TypesInfo.ObjectOf(qualifier).(*types.PkgName)
			if !isPackageName {
				return fmt.Sprintf("(%s).%s", context.TypesInfo.TypeOf(qualifier), selector.Sel.Name)
			}
			return fmt.Sprintf("%s.%s", imported.Imported().Path(), selector.Sel.Name)
		case *ast.SelectorExpr:
			return fmt.Sprintf("(%s).%s", SelectorName(context, qualifier), selector.Sel.Name)
		default:
			// Invariant: only an identifier can qualify a package's member, and a selector of anything else has a
			// selection, so this is a selector the type checker did not check, which staticcheck refuses alike.
			panic(fmt.Sprintf("unsupported selector: %v", selector))
		}
	}
	if field, isVariable := selection.Obj().(*types.Var); isVariable && field.IsField() {
		return fmt.Sprintf("(%s).%s", typeutil.DereferenceR(selection.Recv()), selection.Obj().Name())
	}
	return fmt.Sprintf("(%s).%s", selection.Recv(), selection.Obj().Name())
}
