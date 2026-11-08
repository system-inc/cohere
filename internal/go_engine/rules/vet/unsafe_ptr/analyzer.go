// Package unsafe_ptr is vet's unsafeptr pass, a conversion of a uintptr to unsafe.Pointer that the rules of
// unsafe.Pointer forbid, and a reflect.SliceHeader or reflect.StringHeader made or read as a value. A uintptr is an
// integer the garbage collector and stack copying never see as a pointer, so a conversion back is valid only where
// the uintptr was just made from a pointer, in the same expression: a pointer converted and offset (rule 3), the
// result of reflect.Value's Pointer or UnsafeAddr (rule 5), or the Data field of a header reached through a pointer,
// which approximates a header that overlays a real slice or string (rule 6). A header held as a value, by
// dereferencing a pointer to one or taking the address of one, holds its Data where nothing sees it as a pointer.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unsafeptr/unsafeptr.go, whose
// logic it keeps, isSafeArith exactly as written, and whose messages it reports word for word, over the same range.
// It walks the three kinds of node the original inspects in one preorder, as the original's Preorder does, through
// inspector's public PreorderSeq. The registry runs the port in the original's place, and the original stays the
// oracle: the parity tests beside this file run both over the same packages.
//
// The two differ only where the original reports valid code, each case in testdata/src/differences and held by
// TestPortDiffersFromOriginalOnlyWhereItIsRight, and the port reports nothing the original does not. For rules 5 and
// 6 the original decides by the receiver's type name: a call is reflect.Value's Pointer or UnsafeAddr only on a
// receiver of type reflect.Value written as a selector, and a Data field is a header's only on an expression whose
// type is a pointer to a header. The port decides by what the call or selector resolves to, so it allows the same
// method reached through a pointer to a reflect.Value, an embedded one, a method expression or parentheses, and a
// header's Data reached through a pointer however it is written: an embedded pointer to a header, a defined pointer
// type, or an explicit dereference. A header held as a value, a field of one, an element of an array or slice of
// them, or one embedded by value, is still reported, as is a call through a function value, which the port cannot
// resolve.
package unsafe_ptr

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
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
		// reached through a pointer to a header, and not one of a header value, and so does the port, judging the
		// field the selector resolves to rather than the type of the expression written before it.
		if isHeaderDataThroughPointer(information, unparenthesized) {
			return true
		}
	case *ast.CallExpr:
		// "(5) Conversion of the result of reflect.Value.Pointer or reflect.Value.UnsafeAddr from uintptr to
		// Pointer." The call is judged by the method it statically resolves to, however its receiver is reached.
		if isReflectValueAddress(typeutil.StaticCallee(information, unparenthesized)) {
			return true
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

// isReflectValueAddress reports whether function is reflect.Value's Pointer or UnsafeAddr, whose uintptr result rule 5
// allows converted back at once. Both are declared on a Value receiver, never a pointer, and a call through an
// interface or a function value resolves to no static function, so function is nil there.
func isReflectValueAddress(function *types.Func) bool {
	if function == nil || function.Pkg() == nil || function.Pkg().Path() != "reflect" {
		return false
	}
	if function.Name() != "Pointer" && function.Name() != "UnsafeAddr" {
		return false
	}
	receiver := function.Signature().Recv()
	return receiver != nil && isTypeNamed(receiver.Type(), "reflect", "Value")
}

// isHeaderDataThroughPointer reports whether selector selects the Data field of a reflect.SliceHeader or
// reflect.StringHeader that is reached through a pointer: the header that declares the field is held by a pointer,
// whether that is the selector's receiver or the last field embedded on the way to it, or the receiver is written as
// an explicit dereference, `(*header).Data`. A header held by value, in a variable, a field, an element or an
// embedding by value, is not.
func isHeaderDataThroughPointer(information *types.Info, selector *ast.SelectorExpr) bool {
	selection, isSelection := information.Selections[selector]
	if !isSelection || selection.Kind() != types.FieldVal {
		return false
	}
	// holder is the type that holds the header declaring the selected field: the receiver's type, then, for a field
	// promoted from an embedded one, each embedded field's type along the selection's path but its last step, the
	// field itself.
	holder := selection.Recv()
	path := selection.Index()
	for _, index := range path[:len(path)-1] {
		structure, isStructure := pointedTo(holder).Underlying().(*types.Struct)
		if !isStructure {
			// Invariant: every step but the last of a field selection's path is an embedded field of a struct.
			panic("unsafe_ptr: a field selection passes through a type that is not a struct")
		}
		holder = structure.Field(index).Type()
	}
	if !isReflectHeader(pointedTo(holder)) {
		return false
	}
	if _, isPointer := holder.Underlying().(*types.Pointer); isPointer {
		return true
	}
	_, isDereference := ast.Unparen(selector.X).(*ast.StarExpr)
	return len(path) == 1 && isDereference
}

// pointedTo is the type candidate points to when it is, underneath, a pointer, and candidate itself otherwise.
func pointedTo(candidate types.Type) types.Type {
	if pointer, isPointer := candidate.Underlying().(*types.Pointer); isPointer {
		return pointer.Elem()
	}
	return candidate
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
