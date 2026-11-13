package build_dependence

import (
	"go/ast"
	"go/token"
	"go/types"
)

// bodyWalk turns a unit's body or initializers into its references (rule 4), so a build judges the body by its bits
// without the body. Every use counts by name: an object whose own bit is set, an expression whose type reaches a name
// bit. A type's underlying bit counts only where the use touches the underlying (see touch). The counter judged each
// body per build and stopped at the first dependent use; a unit is dependent in a build exactly when one of the
// references collected here is set in it, so collecting them all once decides the same.
type bodyWalk struct {
	typesInformation *types.Info
	walker           *bitWalker

	// typeReferences are the bits each type reaches in each mode, walked once for the package.
	typeReferences map[walkKey][]reference

	// collected are the current body's references, each once.
	collected map[reference]bool
}

// newBodyWalk is a walk of the bodies of the package typesInformation describes, walking types with walker.
func newBodyWalk(typesInformation *types.Info, walker *bitWalker) *bodyWalk {
	return &bodyWalk{
		typesInformation: typesInformation,
		walker:           walker,
		typeReferences:   map[walkKey][]reference{},
		collected:        map[reference]bool{},
	}
}

// references are what bodies read, each object once with the bit the read touches.
func (walk *bodyWalk) references(bodies ...ast.Node) []reference {
	clear(walk.collected)
	for _, body := range bodies {
		ast.Inspect(body, func(node ast.Node) bool {
			if node != nil {
				walk.visit(node)
			}
			return true
		})
	}
	if len(walk.collected) == 0 {
		return nil
	}
	collected := make([]reference, 0, len(walk.collected))
	for read := range walk.collected {
		collected = append(collected, read)
	}
	return collected
}

// visit collects what one node of a body reads: its own object or selection, what it touches of an underlying, and
// for an expression its type by name, a constant's type in full, and a map's key type in full.
func (walk *bodyWalk) visit(node ast.Node) {
	switch node := node.(type) {
	case *ast.Ident:
		object := walk.typesInformation.Uses[node]
		if object == nil {
			object = walk.typesInformation.Defs[node]
		}
		if object != nil {
			walk.addObject(object)
		}
		// A type argument at any instantiation, since constraint satisfaction reads the underlying.
		if instance, isInstance := walk.typesInformation.Instances[node]; isInstance {
			for argument := range instance.TypeArgs.Types() {
				walk.addType(argument, true)
			}
		}
	case *ast.SelectorExpr:
		// A method selection, promoted ones through embedded fields included, counts through the method's own bit,
		// which carries its receiver's name bit and its signature. A field selection also touches the underlying of
		// each type on its way.
		if selection := walk.typesInformation.Selections[node]; selection != nil {
			walk.addObject(selection.Obj())
			if selection.Kind() == types.FieldVal {
				walk.addFieldPath(selection)
			}
		}
	default:
		walk.touch(node)
	}
	expression, isExpression := node.(ast.Expr)
	if !isExpression {
		return
	}
	typeAndValue, isRecorded := walk.typesInformation.Types[expression]
	if !isRecorded {
		return
	}
	walk.addType(typeAndValue.Type, false)
	// Any constant expression of the type. go/types records an untyped constant with the type it is converted to,
	// so this covers assigning or passing one.
	if typeAndValue.Value != nil {
		walk.addType(typeAndValue.Type, true)
	}
	// Any expression of a map type whose key is underlying-dependent, since hashing reads the key's layout.
	if mapping, isMap := typeAndValue.Type.Underlying().(*types.Map); isMap {
		walk.addType(mapping.Key(), true)
	}
}

// touch collects the underlying a node reads, beyond the cases the briefed rule named: every binary operator except &&
// and ||, every unary operator except &, receive included, a dereference, an index or slice, a range, a type assertion
// or a type switch's cases, an expression switch's tag, a send, ++ and --, an assignment operator, a composite literal
// and a call (touchCall). Passing, assigning, returning or taking the address of a value, new(T) and declaring a var of
// T do not count: none of them reads what lies under the type.
func (walk *bodyWalk) touch(node ast.Node) {
	typeOf := walk.typesInformation.TypeOf
	switch node := node.(type) {
	case *ast.CompositeLit:
		walk.addType(typeOf(node), true)
	case *ast.CallExpr:
		walk.touchCall(node)
	case *ast.BinaryExpr:
		if node.Op != token.LAND && node.Op != token.LOR {
			walk.addType(typeOf(node.X), true)
			walk.addType(typeOf(node.Y), true)
		}
	case *ast.UnaryExpr:
		if node.Op != token.AND {
			walk.addType(typeOf(node.X), true)
		}
	case *ast.StarExpr:
		if operand, isRecorded := walk.typesInformation.Types[node.X]; isRecorded && operand.IsValue() {
			walk.addType(operand.Type, true)
		}
	case *ast.IndexExpr:
		// An index of a generic function is its instantiation, judged by its type arguments (visit).
		if operand, isRecorded := walk.typesInformation.Types[node.X]; isRecorded && operand.IsValue() {
			if _, isSignature := operand.Type.Underlying().(*types.Signature); !isSignature {
				walk.addType(operand.Type, true)
			}
		}
	case *ast.SliceExpr:
		walk.addType(typeOf(node.X), true)
	case *ast.RangeStmt:
		walk.addType(typeOf(node.X), true)
	case *ast.TypeAssertExpr:
		if node.Type != nil {
			walk.addType(typeOf(node.Type), true)
		}
	case *ast.TypeSwitchStmt:
		for _, statement := range node.Body.List {
			if clause, isClause := statement.(*ast.CaseClause); isClause {
				for _, expression := range clause.List {
					walk.addType(typeOf(expression), true)
				}
			}
		}
	case *ast.SwitchStmt:
		if node.Tag != nil {
			walk.addType(typeOf(node.Tag), true)
		}
	case *ast.SendStmt:
		walk.addType(typeOf(node.Chan), true)
	case *ast.IncDecStmt:
		walk.addType(typeOf(node.X), true)
	case *ast.AssignStmt:
		if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
			for _, left := range node.Lhs {
				walk.addType(typeOf(left), true)
			}
		}
	}
}

// touchCall collects the underlying a call reads: a conversion's type and its operand's, every builtin's arguments
// except new's, panic's, print's, println's and recover's (so append, copy, delete, clear, min, max and make count
// alongside len, cap and unsafe's), and the callee's type when it is a named func type.
func (walk *bodyWalk) touchCall(call *ast.CallExpr) {
	function := walk.typesInformation.Types[call.Fun]
	if function.IsType() {
		walk.addType(function.Type, true)
		for _, argument := range call.Args {
			walk.addType(walk.typesInformation.TypeOf(argument), true)
		}
		return
	}
	var callee ast.Expr = ast.Unparen(call.Fun)
	if selector, isSelector := callee.(*ast.SelectorExpr); isSelector {
		callee = selector.Sel
	}
	if identifier, isIdentifier := callee.(*ast.Ident); isIdentifier {
		if builtin, isBuiltin := walk.typesInformation.Uses[identifier].(*types.Builtin); isBuiltin {
			switch builtin.Name() {
			case "new", "panic", "print", "println", "recover":
				return
			}
			for _, argument := range call.Args {
				walk.addType(walk.typesInformation.TypeOf(argument), true)
			}
			return
		}
	}
	switch function.Type.(type) {
	case *types.Named, *types.Alias:
		walk.addType(function.Type, true)
	}
}

// addFieldPath collects the underlying of each type a field selection's path goes through: its receiver, then each
// embedded field it passes, each pointer followed, up to the struct the field is selected from.
func (walk *bodyWalk) addFieldPath(selection *types.Selection) {
	current := selection.Recv()
	for _, index := range selection.Index() {
		if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		walk.addType(current, true)
		structure, isStruct := current.Underlying().(*types.Struct)
		if !isStruct || index >= structure.NumFields() {
			return
		}
		current = structure.Field(index).Type()
	}
}

// addObject collects a use of object on its own: its own bit, as its generic declares it. A local is read through its
// type, a local constant's in full, since no unit declares a local. A package name, a label, a builtin, nil and an
// object of the universe are the same in every build.
func (walk *bodyWalk) addObject(object types.Object) {
	switch object.(type) {
	case *types.PkgName, *types.Label, *types.Builtin, *types.Nil:
		return
	}
	declared := canonical(object)
	switch {
	case universal(declared):
	case local(declared):
		_, isConstant := declared.(*types.Const)
		walk.addType(declared.Type(), isConstant)
	default:
		walk.collected[reference{object: declared}] = true
	}
}

// addType collects the bits walked reaches, read in full or as a name.
func (walk *bodyWalk) addType(walked types.Type, full bool) {
	if walked == nil {
		return
	}
	key := walkKey{walked: walked, full: full}
	referenced, isWalked := walk.typeReferences[key]
	if !isWalked {
		referenced = walk.walker.references(walked, full)
		walk.typeReferences[key] = referenced
	}
	for _, read := range referenced {
		walk.collected[read] = true
	}
}
