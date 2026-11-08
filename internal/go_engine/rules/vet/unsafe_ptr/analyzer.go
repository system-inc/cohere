// Package unsafe_ptr is vet's unsafeptr pass, a conversion of a uintptr to unsafe.Pointer that the rules of
// unsafe.Pointer forbid, and a reflect.SliceHeader or reflect.StringHeader made or read as a value. A uintptr is an
// integer the garbage collector and stack copying never see as a pointer, so a conversion back is valid only where
// the uintptr was just made from a pointer, in the same expression: a pointer converted and offset (rule 3), the
// result of reflect.Value's Pointer or UnsafeAddr (rule 5), or the Data field of a header reached through a pointer,
// which approximates a header that overlays a real slice or string (rule 6). A header held as a value, by
// dereferencing a pointer to one or taking the address of one, holds its Data where nothing sees it as a pointer.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unsafeptr/unsafeptr.go, whose
// logic it keeps, the allowed patterns of isSafeUintptr and isSafeArith exactly as written, and whose messages it
// reports word for word, over the same range. It walks the three kinds of node the original inspects in one preorder,
// as the original's Preorder does, through inspector's public PreorderSeq. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package unsafe_ptr

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "unsafeptr",
	Doc: "check for invalid conversions of uintptr to unsafe.Pointer\n\n" +
		"The unsafeptr analyzer reports likely incorrect uses of unsafe.Pointer\n" +
		"to convert integers to pointers. A conversion from uintptr to\n" +
		"unsafe.Pointer is invalid if it implies that there is a uintptr-typed\n" +
		"word in memory that holds a pointer value, because that word will be\n" +
		"invisible to stack copying and to the garbage collector.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unsafeptr",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("unsafe_ptr: inspect's result is missing")
	}
	// PreorderSeq visits the calls, dereferences and unary expressions in one preorder, as the original's Preorder
	// over the same three kinds does, and the switch hands each over typed.
	for node := range inspected.PreorderSeq((*ast.CallExpr)(nil), (*ast.StarExpr)(nil), (*ast.UnaryExpr)(nil)) {
		switch inspectedNode := node.(type) {
		case *ast.CallExpr:
			// A conversion to unsafe.Pointer, or to a type defined on it, takes one argument; a call of a function
			// value has a signature, never a basic type, so only a conversion is judged.
			if len(inspectedNode.Args) == 1 &&
				hasBasicType(pass.TypesInfo, inspectedNode.Fun, types.UnsafePointer) &&
				hasBasicType(pass.TypesInfo, inspectedNode.Args[0], types.Uintptr) &&
				!isSafeUintptr(pass.TypesInfo, inspectedNode.Args[0]) {
				pass.ReportRangef(inspectedNode, "possible misuse of unsafe.Pointer")
			}
		case *ast.StarExpr:
			// A dereference of a pointer to a header has the header's type, and the pointer type written as a type
			// expression, `*reflect.SliceHeader`, has the pointer's, so only the dereference is judged.
			if dereferenced := pass.TypesInfo.Types[inspectedNode].Type; isReflectHeader(dereferenced) {
				pass.ReportRangef(inspectedNode, "possible misuse of %s", dereferenced)
			}
		case *ast.UnaryExpr:
			if inspectedNode.Op != token.AND {
				continue
			}
			if addressed := pass.TypesInfo.Types[inspectedNode.X].Type; isReflectHeader(addressed) {
				pass.ReportRangef(inspectedNode, "possible misuse of %s", addressed)
			}
		}
	}
	return nil, nil
}

// isSafeUintptr reports whether expression, already known to be a uintptr, is safe to convert to unsafe.Pointer,
// by the rules of https://golang.org/pkg/unsafe/#Pointer.
func isSafeUintptr(information *types.Info, expression ast.Expr) bool {
	switch unparenthesized := ast.Unparen(expression).(type) {
	case *ast.SelectorExpr:
		// "(6) Conversion of a reflect.SliceHeader or reflect.StringHeader Data field to or from Pointer."
		if unparenthesized.Sel.Name != "Data" {
			break
		}
		// A header is okay only while it points at a real slice or string. A header variable whose Data was set from
		// a pointer is not: between the set and the conversion, the garbage collector does not see Data as a pointer,
		// so it may dangle by the time it is converted. The original approximates that by allowing a Data field
		// reached through a pointer to a header, and not one of a header value, and so does the port.
		pointer, isPointer := types.Unalias(information.Types[unparenthesized.X].Type).(*types.Pointer)
		if isPointer && isReflectHeader(pointer.Elem()) {
			return true
		}
	case *ast.CallExpr:
		// "(5) Conversion of the result of reflect.Value.Pointer or reflect.Value.UnsafeAddr from uintptr to
		// Pointer." The method is matched on a selector written directly as the call's function, and on a receiver
		// of type reflect.Value itself, as the original matches it.
		if len(unparenthesized.Args) != 0 {
			break
		}
		selector, isSelector := unparenthesized.Fun.(*ast.SelectorExpr)
		if !isSelector {
			break
		}
		switch selector.Sel.Name {
		case "Pointer", "UnsafeAddr":
			if isTypeNamed(information.Types[selector.X].Type, "reflect", "Value") {
				return true
			}
		}
	}
	// "(3) Conversion of a Pointer to a uintptr and back, with arithmetic."
	return isSafeArithmetic(information, expression)
}

// isSafeArithmetic reports whether expression is pointer arithmetic that is safe to convert to unsafe.Pointer: a
// pointer just converted to uintptr, offset by adding, subtracting or rounding with `&^`. x/tools' isSafeArith.
func isSafeArithmetic(information *types.Info, expression ast.Expr) bool {
	switch unparenthesized := ast.Unparen(expression).(type) {
	case *ast.CallExpr:
		// The base case: the conversion of an unsafe.Pointer to uintptr.
		return len(unparenthesized.Args) == 1 &&
			hasBasicType(information, unparenthesized.Fun, types.Uintptr) &&
			hasBasicType(information, unparenthesized.Args[0], types.UnsafePointer)
	case *ast.BinaryExpr:
		// "It is valid both to add and to subtract offsets from a pointer in this way. It is also valid to use &^ to
		// round pointers, usually for alignment." The pointer must be on the left and only there, so a sum with the
		// pointer on the right, `1 + uintptr(pointer)`, valid to the compiler, is still reported, to be written with
		// the pointer first, and a sum of two pointers is reported too, as the original reports both.
		switch unparenthesized.Op {
		case token.ADD, token.SUB, token.AND_NOT:
			return isSafeArithmetic(information, unparenthesized.X) && !isSafeArithmetic(information, unparenthesized.Y)
		}
	}
	return false
}

// hasBasicType reports whether expression's type is, underneath, the basic type of the given kind. An expression
// with no recorded type, a package name, has none.
func hasBasicType(information *types.Info, expression ast.Expr, kind types.BasicKind) bool {
	recorded := information.Types[expression].Type
	if recorded == nil {
		return false
	}
	basic, isBasic := recorded.Underlying().(*types.Basic)
	return isBasic && basic.Kind() == kind
}

// isReflectHeader reports whether candidate is reflect.SliceHeader or reflect.StringHeader, or an alias of one.
func isReflectHeader(candidate types.Type) bool {
	return isTypeNamed(candidate, "reflect", "SliceHeader", "StringHeader")
}

// isTypeNamed reports whether candidate is, or aliases, a type declared at package level in the package at
// packagePath under one of names. x/tools' typesinternal.IsTypeNamed.
func isTypeNamed(candidate types.Type, packagePath string, names ...string) bool {
	named, isNamed := types.Unalias(candidate).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil && isPackageLevel(typeName) && typeName.Pkg().Path() == packagePath &&
		slices.Contains(names, typeName.Name())
}

// isPackageLevel reports whether object is declared in its package's scope; a predeclared object such as `error`
// belongs to no package. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
