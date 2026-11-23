package guard

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/system-inc/go-types/types"
)

// syntaxTreeWriteReader reads one package's files.
type syntaxTreeWriteReader struct {
	scan    *syntaxTreeWriteScan
	fileSet *token.FileSet
	info    *types.Info

	// fresh are the package's local variables that only ever hold what their function built, and freshResults its
	// functions of one result that only ever return what they built (freshness).
	fresh        map[*types.Var]bool
	freshResults map[*types.Func]bool
}

func (reader *syntaxTreeWriteReader) inspect(node ast.Node) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		if typed.Tok != token.DEFINE {
			for _, target := range typed.Lhs {
				reader.target(target, "writes")
			}
		}
	case *ast.IncDecStmt:
		reader.target(typed.X, "writes")
	case *ast.RangeStmt:
		if typed.Tok == token.ASSIGN {
			for _, target := range []ast.Expr{typed.Key, typed.Value} {
				if target != nil {
					reader.target(target, "writes")
				}
			}
		}
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			reader.addressTarget(typed.X)
		}
	case *ast.CallExpr:
		reader.call(typed)
	}
}

// accessStep is one step of the path from a variable to the place a write lands, outermost first.
type accessStep struct {
	// label is what the step reaches: `Type.field`, `an element`, `an entry`, `the target`.
	label string

	// treeField is the step's `Type.Field` when it selects a field of a struct the ast package declares.
	treeField string

	// treeElement says what the step reaches when it is an element, entry or target of the tree's own type.
	treeElement string

	// indirect is a step that follows a pointer, a slice or a map, so what it reaches is not the storage of
	// what it started from.
	indirect bool
}

// accessPath is the steps from target's root to target, outermost first, and the root: the expression it starts
// from, an identifier when the path starts at a variable.
func (reader *syntaxTreeWriteReader) accessPath(target ast.Expr) (ast.Expr, []accessStep) {
	var steps []accessStep
	expression := target
	for {
		switch typed := expression.(type) {
		case *ast.ParenExpr:
			expression = typed.X
			continue
		case *ast.SelectorExpr:
			selection := reader.info.Selections[typed]
			if selection == nil || selection.Kind() != types.FieldVal {
				return expression, steps
			}
			field := selection.Obj()
			step := accessStep{label: typeName(selection.Recv()) + "." + field.Name(), indirect: selection.Indirect()}
			if field.Pkg() != nil && syntaxTreePackages[field.Pkg().Path()] {
				step.treeField = step.label
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		case *ast.IndexExpr:
			container := reader.info.TypeOf(typed.X)
			if container == nil {
				return expression, steps
			}
			step, isElement := elementStep(container)
			if !isElement {
				return expression, steps
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		case *ast.SliceExpr:
			container := reader.info.TypeOf(typed.X)
			if container == nil {
				return expression, steps
			}
			_, isArray := container.Underlying().(*types.Array)
			steps = append(steps, accessStep{label: "a slice", indirect: !isArray})
			expression = typed.X
			continue
		case *ast.StarExpr:
			pointee := reader.info.TypeOf(typed)
			if pointee == nil {
				return expression, steps
			}
			// A pointer to a pointer, slice or map points into the tree only at an address taken of the tree's
			// field, which is reported where it is taken; a pointer to the tree's own struct is a node, a list.
			step := accessStep{label: "the target", indirect: true}
			if isSyntaxTreeType(pointee) {
				step.treeElement = "the " + typeString(pointee) + " a " + typeString(reader.info.TypeOf(typed.X)) +
					" points to"
			}
			steps = append(steps, step)
			expression = typed.X
			continue
		}
		return expression, steps
	}
}

// elementStep is the step an index into container takes, and false when container is not indexed for storage (a
// string, or a generic function's instantiation).
func elementStep(container types.Type) (accessStep, bool) {
	switch typed := container.Underlying().(type) {
	case *types.Slice:
		step := accessStep{label: "an element", indirect: true}
		if namesTheSyntaxTree(typed.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Array:
		step := accessStep{label: "an element"}
		if namesTheSyntaxTree(typed.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Pointer:
		array, isArray := typed.Elem().Underlying().(*types.Array)
		if !isArray {
			return accessStep{}, false
		}
		step := accessStep{label: "an element", indirect: true}
		if namesTheSyntaxTree(array.Elem(), 0) {
			step.treeElement = "an element of a " + typeString(container)
		}
		return step, true
	case *types.Map:
		// A map is the tree's only as a type the ast package declares (ast.SymbolTable): an entry written into a
		// map the rule keeps of nodes writes the rule's map, not the tree.
		step := accessStep{label: "an entry", indirect: true}
		if isSyntaxTreeType(container) {
			step.treeElement = "an entry of an " + typeString(container)
		}
		return step, true
	}
	return accessStep{}, false
}

// target reports target, a place written or whose address is taken, when the place is the tree's.
func (reader *syntaxTreeWriteReader) target(target ast.Expr, verb string) {
	root, steps := reader.accessPath(target)
	reader.report(target.Pos(), verb, root, steps)
}

// addressTarget reports a place whose address is taken, when the place is the tree's. A place that is itself one of
// the ast package's structs is left alone: `&statement.Node` is the node every listener is handed, and a write
// through it selects a field of the tree, which is reported where it is written. What else the tree holds (a list
// pointer, a slice, flags, a range) has no field to be written through, so its address is the write's door.
func (reader *syntaxTreeWriteReader) addressTarget(place ast.Expr) {
	if placeType := reader.info.TypeOf(place); placeType != nil && isSyntaxTreeType(placeType) {
		if _, isStruct := placeType.Underlying().(*types.Struct); isStruct {
			return
		}
	}
	reader.target(place, "takes the address of")
}

// report records the place steps reach from root, under verb, unless it is not the tree's or is the function's own.
func (reader *syntaxTreeWriteReader) report(position token.Pos, verb string, root ast.Expr, steps []accessStep) {
	description := describeSteps(steps)
	if description == "" || reader.ownStorage(root, steps) {
		return
	}
	reader.record(position, verb+" "+description)
}

func (reader *syntaxTreeWriteReader) record(position token.Pos, what string) {
	location := reader.fileSet.Position(position)
	path := location.Filename
	if relative, relativeError := filepath.Rel(reader.scan.root, path); relativeError == nil {
		path = filepath.ToSlash(relative)
	}
	reader.scan.writes = append(reader.scan.writes, path+":"+strconv.Itoa(location.Line)+" "+what)
}

// describeSteps says what a path reaches of the tree, and nothing when it reaches nothing of it: the outermost field
// of a struct the ast package declares, what of it is reached past that, or else the tree-typed element or target.
func describeSteps(steps []accessStep) string {
	for index, step := range steps {
		if step.treeField == "" {
			continue
		}
		if index > 0 {
			return steps[0].label + " of " + step.treeField
		}
		return step.treeField
	}
	for _, step := range steps {
		if step.treeElement != "" {
			return step.treeElement
		}
	}
	return ""
}

// ownStorage reports a path whose storage is the function's own: a local variable's, reached with no pointer, slice
// or map followed, or the one allocation a fresh variable holds.
func (reader *syntaxTreeWriteReader) ownStorage(root ast.Expr, steps []accessStep) bool {
	identifier, isIdentifier := root.(*ast.Ident)
	if !isIdentifier {
		return false
	}
	variable := reader.localVariable(identifier)
	if variable == nil {
		return false
	}
	indirections := 0
	for _, step := range steps {
		if step.indirect {
			indirections++
		}
	}
	if indirections == 0 {
		return true
	}
	if indirections > 1 || !reader.fresh[variable] {
		return false
	}
	// The one indirection a fresh variable's own allocation takes is the first step from it, through the pointer,
	// slice or map it holds.
	switch variable.Type().Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map:
		return steps[len(steps)-1].indirect
	}
	return false
}

// localVariable is the variable identifier names when it is one declared inside a function, and nil otherwise.
func (reader *syntaxTreeWriteReader) localVariable(identifier *ast.Ident) *types.Var {
	variable, isVariable := reader.info.ObjectOf(identifier).(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() == nil || variable.Parent() == nil ||
		variable.Parent() == variable.Pkg().Scope() {
		return nil
	}
	return variable
}

// call reports a call that writes the tree: one of the ast package's writers, a builtin or standard function that
// writes a tree slice, or a pointer method called on a place of the tree, which takes its address.
func (reader *syntaxTreeWriteReader) call(call *ast.CallExpr) {
	callee := reader.callee(call)
	switch typed := callee.(type) {
	case *types.Builtin:
		if len(call.Args) == 0 {
			return
		}
		switch typed.Name() {
		case "copy":
			reader.sliceWrite(call.Args[0], "copies into", true)
		case "append":
			// Appending onto a slice the function was handed is the common case and writes past its length, which
			// no reader of the tree looks at; onto a field of the tree it writes the tree's spare capacity, and
			// onto a slice of one (`list.Nodes[:i]`) it writes the elements after i.
			reader.sliceWrite(call.Args[0], "appends onto", false)
		}
		return
	case *types.Func:
		function := typed
		if function.Pkg() == nil {
			return
		}
		signature, isSignature := function.Type().(*types.Signature)
		if !isSignature {
			return
		}
		qualified := function.Pkg().Path() + "." + function.Name()
		if inPlaceSliceFunctions[qualified] && len(call.Args) > 0 {
			reader.sliceWrite(call.Args[0], "writes in place, by "+function.Pkg().Name()+"."+function.Name()+",",
				true)
			return
		}
		if syntaxTreePackages[function.Pkg().Path()] && syntaxTreeWriters[function.Name()] {
			reader.writerCall(call, function, signature)
			return
		}
		if signature.Recv() != nil {
			reader.pointerMethodCall(call)
		}
	}
}

// callee is the function or builtin call calls, and nil for a conversion or a call through a function value.
func (reader *syntaxTreeWriteReader) callee(call *ast.CallExpr) types.Object {
	switch function := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return reader.info.Uses[function]
	case *ast.SelectorExpr:
		return reader.info.Uses[function.Sel]
	case *ast.IndexExpr:
		// An explicit instantiation, `slices.SortFunc[...]`.
		switch generic := ast.Unparen(function.X).(type) {
		case *ast.Ident:
			return reader.info.Uses[generic]
		case *ast.SelectorExpr:
			return reader.info.Uses[generic.Sel]
		}
	}
	return nil
}

// sliceWrite reports a call that writes slice's storage, when it is the tree's. With elements set the call writes
// slice's elements, the tree's when slice is reached through a field of the tree or holds the tree's type and is not
// a slice the function built. Without it the call writes past what slice holds, the tree's only when slice is
// reached through a field of the tree: `kept.Nodes = append(kept.Nodes, member)` onto a fresh list is the rule's
// own.
func (reader *syntaxTreeWriteReader) sliceWrite(slice ast.Expr, verb string, elements bool) {
	root, steps := reader.accessPath(slice)
	sliceType := reader.info.TypeOf(slice)
	if sliceType == nil {
		return
	}
	if elements {
		step := accessStep{label: "an element", indirect: true}
		if element, isSlice := sliceType.Underlying().(*types.Slice); isSlice && namesTheSyntaxTree(element.Elem(), 0) {
			step.treeElement = "a " + typeString(sliceType)
		}
		steps = append([]accessStep{step}, steps...)
	}
	reader.report(slice.Pos(), verb, root, steps)
}

// writerCall reports a call to one of the ast package's writers, unless what it writes is the function's own: a
// method's receiver, or a function's first pointer argument. A writer handed no pointer writes the package's own
// state, which every checker reads.
func (reader *syntaxTreeWriteReader) writerCall(call *ast.CallExpr, function *types.Func, signature *types.Signature) {
	name := function.Name()
	var written ast.Expr
	var writtenType types.Type
	if receiver := signature.Recv(); receiver != nil {
		name = typeName(receiver.Type()) + "." + name
		if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
			written, writtenType = selector.X, reader.info.TypeOf(selector.X)
		}
	} else {
		name = "ast." + name
		for index := 0; index < signature.Params().Len() && index < len(call.Args); index++ {
			if _, isPointer := signature.Params().At(index).Type().Underlying().(*types.Pointer); isPointer {
				written, writtenType = call.Args[index], reader.info.TypeOf(call.Args[index])
				break
			}
		}
	}
	if written == nil || writtenType == nil {
		reader.record(call.Pos(), "calls "+name+", which writes the ast package's own state")
		return
	}
	root, steps := reader.accessPath(written)
	_, isPointer := writtenType.Underlying().(*types.Pointer)
	writtenStep := accessStep{label: "the target", treeElement: "the " + typeString(writtenType), indirect: isPointer}
	steps = append([]accessStep{writtenStep}, steps...)
	if reader.ownStorage(root, steps) {
		return
	}
	reader.record(call.Pos(), "calls "+name+", which writes "+describeSteps(steps))
}

// pointerMethodCall reports a pointer method called on an addressable place of the tree, which takes the place's
// address as a write would: `node.Loc.Set(...)` were TextRange's methods pointer ones.
func (reader *syntaxTreeWriteReader) pointerMethodCall(call *ast.CallExpr) {
	if place, isPlace := reader.pointerMethodReceiver(call); isPlace {
		reader.addressTarget(place)
	}
}

// pointerMethodReceiver is the place a call takes the address of to call a pointer method on it, a value rather
// than a pointer, and false for any other call.
func (reader *syntaxTreeWriteReader) pointerMethodReceiver(call *ast.CallExpr) (ast.Expr, bool) {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return nil, false
	}
	selection := reader.info.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil, false
	}
	signature, isSignature := selection.Obj().Type().(*types.Signature)
	if !isSignature || signature.Recv() == nil {
		return nil, false
	}
	if _, pointerReceiver := signature.Recv().Type().Underlying().(*types.Pointer); !pointerReceiver {
		return nil, false
	}
	receiverType := reader.info.TypeOf(selector.X)
	if receiverType == nil {
		return nil, false
	}
	if _, isPointer := receiverType.Underlying().(*types.Pointer); isPointer {
		return nil, false
	}
	return selector.X, true
}

// freshness finds the package's local variables that only ever hold what their function built, and its functions
// that only ever return what they built, to a fixed point: a variable assigned from another, or from a call, holds
// what that one does, and a function returning a variable returns what the variable holds. A variable whose address
// is taken, by `&` or a pointer method called on it, is not fresh, since what the address is handed to may store
// anything there.
func (reader *syntaxTreeWriteReader) freshness(files []*ast.File) {
	values := map[*types.Var][]ast.Expr{}
	stale := map[*types.Var]bool{}
	returns := map[*types.Func][]*ast.ReturnStmt{}
	markFields := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := reader.info.Defs[name].(*types.Var); isVariable {
					stale[variable] = true
				}
			}
		}
	}
	assign := func(identifier *ast.Ident, value ast.Expr, known bool) {
		variable := reader.localVariable(identifier)
		if variable == nil {
			return
		}
		if !known {
			stale[variable] = true
			return
		}
		values[variable] = append(values[variable], value)
	}
	// addressed marks the local variable a place's path starts at, when its address, or a part's, is taken.
	addressed := func(place ast.Expr) {
		root, _ := reader.accessPath(place)
		if identifier, isIdentifier := root.(*ast.Ident); isIdentifier {
			if variable := reader.localVariable(identifier); variable != nil {
				stale[variable] = true
			}
		}
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			object, isFunctionObject := reader.info.Defs[function.Name].(*types.Func)
			if !isFunctionObject {
				continue
			}
			signature, isSignature := object.Type().(*types.Signature)
			if !isSignature || signature.Results().Len() != 1 {
				continue
			}
			found := []*ast.ReturnStmt{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncLit:
					// A function literal's returns are its own.
					return false
				case *ast.ReturnStmt:
					found = append(found, typed)
				}
				return true
			})
			returns[object] = found
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				markFields(typed.Recv)
				markFields(typed.Type.Params)
				markFields(typed.Type.Results)
			case *ast.FuncLit:
				markFields(typed.Type.Params)
				markFields(typed.Type.Results)
			case *ast.AssignStmt:
				if typed.Tok != token.DEFINE && typed.Tok != token.ASSIGN {
					return true
				}
				for index, target := range typed.Lhs {
					if identifier, isIdentifier := ast.Unparen(target).(*ast.Ident); isIdentifier {
						if len(typed.Lhs) == len(typed.Rhs) {
							assign(identifier, typed.Rhs[index], true)
						} else {
							assign(identifier, nil, false)
						}
					}
				}
			case *ast.ValueSpec:
				for index, name := range typed.Names {
					switch len(typed.Values) {
					case 0:
						// The zero value, which is the variable's own.
						assign(name, nil, true)
					case len(typed.Names):
						assign(name, typed.Values[index], true)
					default:
						assign(name, nil, false)
					}
				}
			case *ast.RangeStmt:
				for _, target := range []ast.Expr{typed.Key, typed.Value} {
					if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
						assign(identifier, nil, false)
					}
				}
			case *ast.CaseClause:
				if implicit, isVariable := reader.info.Implicits[typed].(*types.Var); isVariable {
					stale[implicit] = true
				}
			case *ast.UnaryExpr:
				if typed.Op == token.AND {
					addressed(typed.X)
				}
			case *ast.CallExpr:
				if place, isPlace := reader.pointerMethodReceiver(typed); isPlace {
					addressed(place)
				}
			}
			return true
		})
	}
	reader.fresh = map[*types.Var]bool{}
	for variable := range values {
		if !stale[variable] {
			reader.fresh[variable] = true
		}
	}
	reader.freshResults = map[*types.Func]bool{}
	for function := range returns {
		reader.freshResults[function] = true
	}
	for changed := true; changed; {
		changed = false
		for variable := range reader.fresh {
			for _, value := range values[variable] {
				if value != nil && !reader.freshExpression(value) {
					delete(reader.fresh, variable)
					changed = true
					break
				}
			}
		}
		for function := range reader.freshResults {
			for _, statement := range returns[function] {
				// A bare return hands back named results, which are never fresh.
				if len(statement.Results) != 1 || !reader.freshExpression(statement.Results[0]) {
					delete(reader.freshResults, function)
					changed = true
					break
				}
			}
		}
	}
}

// freshExpression reports an expression whose value the function built itself, given which variables and functions
// are fresh so far.
func (reader *syntaxTreeWriteReader) freshExpression(expression ast.Expr) bool {
	switch typed := ast.Unparen(expression).(type) {
	case *ast.CompositeLit, *ast.BasicLit, *ast.FuncLit:
		return true
	case *ast.Ident:
		switch object := reader.info.ObjectOf(typed).(type) {
		case *types.Nil:
			return true
		case *types.Var:
			return reader.fresh[object]
		}
	case *ast.UnaryExpr:
		return typed.Op == token.AND && reader.freshExpression(typed.X)
	case *ast.SliceExpr:
		return reader.freshExpression(typed.X)
	case *ast.CallExpr:
		if function := reader.info.Types[typed.Fun]; function.IsType() {
			return len(typed.Args) == 1 && reader.freshExpression(typed.Args[0])
		}
		switch callee := reader.callee(typed).(type) {
		case *types.Builtin:
			switch callee.Name() {
			case "make", "new":
				return true
			case "append":
				return len(typed.Args) > 0 && reader.freshExpression(typed.Args[0])
			}
		case *types.Func:
			if callee.Pkg() == nil {
				return false
			}
			if freshFunctions[callee.Pkg().Path()+"."+callee.Name()] || reader.freshResults[callee] {
				return true
			}
			signature, isSignature := callee.Type().(*types.Signature)
			return isSignature && signature.Recv() != nil && syntaxTreePackages[callee.Pkg().Path()] &&
				typeName(signature.Recv().Type()) == "NodeFactory" && strings.HasPrefix(callee.Name(), "New")
		}
	}
	return false
}

// namesTheSyntaxTree reports a type that is the tree's, or a pointer, slice or array of one.
func namesTheSyntaxTree(subject types.Type, depth int) bool {
	if subject == nil || depth > 8 {
		return false
	}
	if isSyntaxTreeType(subject) {
		return true
	}
	switch typed := types.Unalias(subject).Underlying().(type) {
	case *types.Pointer:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	case *types.Slice:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	case *types.Array:
		return namesTheSyntaxTree(typed.Elem(), depth+1)
	}
	return false
}

// isSyntaxTreeType reports a named type the ast package declares.
func isSyntaxTreeType(subject types.Type) bool {
	named, isNamed := types.Unalias(subject).(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && syntaxTreePackages[named.Obj().Pkg().Path()]
}

// typeName is a type's own name, through a pointer: `ClassDeclaration` for `*ast.ClassDeclaration`.
func typeName(subject types.Type) string {
	named := subject
	if pointer, isPointer := types.Unalias(subject).(*types.Pointer); isPointer {
		named = pointer.Elem()
	}
	if namedType, isNamed := types.Unalias(named).(*types.Named); isNamed {
		return namedType.Obj().Name()
	}
	return typeString(named)
}

// typeString spells a type with its packages by name, `[]*ast.Node`.
func typeString(subject types.Type) string {
	return types.TypeString(subject, func(typePackage *types.Package) string { return typePackage.Name() })
}
