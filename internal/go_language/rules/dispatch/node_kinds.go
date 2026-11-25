package dispatch

import "go/ast"

// nodeKindCount is one past the highest number nodeKindOf gives a kind, so an array of it holds a slot for each.
const nodeKindCount = 56

// nodeKindOf is the number of node's kind among the kinds of node the inspector walks a file's syntax through, from 1
// in the order of their names, or 0 for a kind it does not number, such as one a later go/ast adds, which the syntax
// walk looks up by its reflect.Type instead. A type switch is a few comparisons of the kind's hash, where a map keyed
// by reflect.Type hashes the interface and compares it on every node visited (#fex6s0v). It leaves out ast.Package,
// deprecated, which is no file's node.
func nodeKindOf(node ast.Node) int {
	switch node.(type) {
	case *ast.ArrayType:
		return 1
	case *ast.AssignStmt:
		return 2
	case *ast.BadDecl:
		return 3
	case *ast.BadExpr:
		return 4
	case *ast.BadStmt:
		return 5
	case *ast.BasicLit:
		return 6
	case *ast.BinaryExpr:
		return 7
	case *ast.BlockStmt:
		return 8
	case *ast.BranchStmt:
		return 9
	case *ast.CallExpr:
		return 10
	case *ast.CaseClause:
		return 11
	case *ast.ChanType:
		return 12
	case *ast.CommClause:
		return 13
	case *ast.Comment:
		return 14
	case *ast.CommentGroup:
		return 15
	case *ast.CompositeLit:
		return 16
	case *ast.DeclStmt:
		return 17
	case *ast.DeferStmt:
		return 18
	case *ast.Ellipsis:
		return 19
	case *ast.EmptyStmt:
		return 20
	case *ast.ExprStmt:
		return 21
	case *ast.Field:
		return 22
	case *ast.FieldList:
		return 23
	case *ast.File:
		return 24
	case *ast.ForStmt:
		return 25
	case *ast.FuncDecl:
		return 26
	case *ast.FuncLit:
		return 27
	case *ast.FuncType:
		return 28
	case *ast.GenDecl:
		return 29
	case *ast.GoStmt:
		return 30
	case *ast.Ident:
		return 31
	case *ast.IfStmt:
		return 32
	case *ast.ImportSpec:
		return 33
	case *ast.IncDecStmt:
		return 34
	case *ast.IndexExpr:
		return 35
	case *ast.IndexListExpr:
		return 36
	case *ast.InterfaceType:
		return 37
	case *ast.KeyValueExpr:
		return 38
	case *ast.LabeledStmt:
		return 39
	case *ast.MapType:
		return 40
	case *ast.ParenExpr:
		return 41
	case *ast.RangeStmt:
		return 42
	case *ast.ReturnStmt:
		return 43
	case *ast.SelectStmt:
		return 44
	case *ast.SelectorExpr:
		return 45
	case *ast.SendStmt:
		return 46
	case *ast.SliceExpr:
		return 47
	case *ast.StarExpr:
		return 48
	case *ast.StructType:
		return 49
	case *ast.SwitchStmt:
		return 50
	case *ast.TypeAssertExpr:
		return 51
	case *ast.TypeSpec:
		return 52
	case *ast.TypeSwitchStmt:
		return 53
	case *ast.UnaryExpr:
		return 54
	case *ast.ValueSpec:
		return 55
	}
	return 0
}
