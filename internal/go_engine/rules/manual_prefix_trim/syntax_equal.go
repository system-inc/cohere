package manual_prefix_trim

import (
	"go/ast"
	"go/token"
	"reflect"
)

// syntaxEqual is whether first and second are the same syntax as honnef.co/go/tools@v0.8.1 go/ast/astutil's Equal
// judges two nodes, which the original compares selectors, index expressions and literals with: by kind, operator and
// spelling, identifiers by name, parentheses kept, positions skipped. It answers as Equal does on every node kind
// Equal handles, its oddities kept, and answers false where Equal panics, a node kind it does not know, such as a
// function type, or a node absent on one side only where both are pointers of one type, such as a field's tag. A
// panic in a check is a crash under its rule's name, so the port declines there where the original crashes.
func syntaxEqual(first ast.Node, second ast.Node) bool {
	// Equal answers true for one node, or two absent ones of the same type, before it reads either.
	if first == second {
		return true
	}
	if isAbsent(first) || isAbsent(second) {
		return false
	}
	if reflect.TypeOf(first) != reflect.TypeOf(second) {
		return false
	}
	switch first := first.(type) {
	case *ast.BasicLit:
		secondLiteral, isLiteral := second.(*ast.BasicLit)
		return isLiteral && first.Kind == secondLiteral.Kind && first.Value == secondLiteral.Value
	case *ast.BinaryExpr:
		secondBinary, isBinary := second.(*ast.BinaryExpr)
		return isBinary && syntaxEqual(first.X, secondBinary.X) && first.Op == secondBinary.Op &&
			syntaxEqual(first.Y, secondBinary.Y)
	case *ast.CallExpr:
		secondCall, isCall := second.(*ast.CallExpr)
		if !isCall || len(first.Args) != len(secondCall.Args) {
			return false
		}
		for position, argument := range first.Args {
			if !syntaxEqual(argument, secondCall.Args[position]) {
				return false
			}
		}
		// A spread last argument on both sides, or on neither, is alike.
		return syntaxEqual(first.Fun, secondCall.Fun) &&
			(first.Ellipsis == token.NoPos) == (secondCall.Ellipsis == token.NoPos)
	case *ast.CompositeLit:
		secondComposite, isComposite := second.(*ast.CompositeLit)
		// Equal compares each of the second literal's elements with itself, so the elements are alike whenever they
		// are as many, whatever they hold, and only their count is compared here.
		return isComposite && len(first.Elts) == len(secondComposite.Elts) &&
			syntaxEqual(first.Type, secondComposite.Type) && first.Incomplete == secondComposite.Incomplete
	case *ast.Ident:
		secondIdentifier, isIdentifier := second.(*ast.Ident)
		return isIdentifier && first.Name == secondIdentifier.Name
	case *ast.IndexExpr:
		secondIndex, isIndex := second.(*ast.IndexExpr)
		return isIndex && syntaxEqual(first.X, secondIndex.X) && syntaxEqual(first.Index, secondIndex.Index)
	case *ast.IndexListExpr:
		secondIndexList, isIndexList := second.(*ast.IndexListExpr)
		if !isIndexList || len(first.Indices) != len(secondIndexList.Indices) {
			return false
		}
		for position, index := range first.Indices {
			if !syntaxEqual(index, secondIndexList.Indices[position]) {
				return false
			}
		}
		return syntaxEqual(first.X, secondIndexList.X)
	case *ast.KeyValueExpr:
		secondKeyValue, isKeyValue := second.(*ast.KeyValueExpr)
		return isKeyValue && syntaxEqual(first.Key, secondKeyValue.Key) &&
			syntaxEqual(first.Value, secondKeyValue.Value)
	case *ast.ParenExpr:
		secondParenthesized, isParenthesized := second.(*ast.ParenExpr)
		return isParenthesized && syntaxEqual(first.X, secondParenthesized.X)
	case *ast.SelectorExpr:
		secondSelector, isSelector := second.(*ast.SelectorExpr)
		return isSelector && syntaxEqual(first.X, secondSelector.X) && syntaxEqual(first.Sel, secondSelector.Sel)
	case *ast.SliceExpr:
		secondSlice, isSlice := second.(*ast.SliceExpr)
		return isSlice && syntaxEqual(first.X, secondSlice.X) && syntaxEqual(first.Low, secondSlice.Low) &&
			syntaxEqual(first.High, secondSlice.High) && syntaxEqual(first.Max, secondSlice.Max) &&
			first.Slice3 == secondSlice.Slice3
	case *ast.StarExpr:
		secondStar, isStar := second.(*ast.StarExpr)
		return isStar && syntaxEqual(first.X, secondStar.X)
	case *ast.TypeAssertExpr:
		secondAssertion, isAssertion := second.(*ast.TypeAssertExpr)
		return isAssertion && syntaxEqual(first.X, secondAssertion.X) && syntaxEqual(first.Type, secondAssertion.Type)
	case *ast.UnaryExpr:
		secondUnary, isUnary := second.(*ast.UnaryExpr)
		return isUnary && first.Op == secondUnary.Op && syntaxEqual(first.X, secondUnary.X)
	case *ast.MapType:
		secondMap, isMap := second.(*ast.MapType)
		return isMap && syntaxEqual(first.Key, secondMap.Key) && syntaxEqual(first.Value, secondMap.Value)
	case *ast.ArrayType:
		secondArray, isArray := second.(*ast.ArrayType)
		return isArray && syntaxEqual(first.Len, secondArray.Len) && syntaxEqual(first.Elt, secondArray.Elt)
	case *ast.Ellipsis:
		secondEllipsis, isEllipsis := second.(*ast.Ellipsis)
		return isEllipsis && syntaxEqual(first.Elt, secondEllipsis.Elt)
	case *ast.InterfaceType:
		secondInterface, isInterface := second.(*ast.InterfaceType)
		return isInterface && first.Incomplete == secondInterface.Incomplete &&
			syntaxEqual(first.Methods, secondInterface.Methods)
	case *ast.StructType:
		secondStruct, isStruct := second.(*ast.StructType)
		return isStruct && first.Incomplete == secondStruct.Incomplete && syntaxEqual(first.Fields, secondStruct.Fields)
	case *ast.FuncLit:
		// Equal leaves function literals to a later version, and finds two never alike.
		return false
	case *ast.ChanType:
		secondChannel, isChannel := second.(*ast.ChanType)
		// Equal compares a channel type's direction and whether its arrow is written, and never its element type.
		return isChannel && first.Dir == secondChannel.Dir &&
			(first.Arrow == token.NoPos) == (secondChannel.Arrow == token.NoPos)
	case *ast.FieldList:
		secondFields, isFields := second.(*ast.FieldList)
		if !isFields || len(first.List) != len(secondFields.List) {
			return false
		}
		for position, field := range first.List {
			if !syntaxEqual(field, secondFields.List[position]) {
				return false
			}
		}
		return true
	case *ast.Field:
		secondField, isField := second.(*ast.Field)
		if !isField || len(first.Names) != len(secondField.Names) {
			return false
		}
		for position, name := range first.Names {
			if !syntaxEqual(name, secondField.Names[position]) {
				return false
			}
		}
		return syntaxEqual(first.Type, secondField.Type) && syntaxEqual(first.Tag, secondField.Tag)
	default:
		// Equal panics on every other kind, and the port declines.
		return false
	}
}

// isAbsent is whether node is missing: no node at all, or a pointer of a node type that points nowhere, which Equal
// receives for a field's absent tag and would read through.
func isAbsent(node ast.Node) bool {
	if node == nil {
		return true
	}
	value := reflect.ValueOf(node)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
