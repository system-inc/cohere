package build_reads

import (
	"go/ast"
	"go/token"
	"go/types"
)

// The kinds of use a package makes of other packages' types, read from where each expression and type expression of
// its syntax sits (#mjdf7ah). A member or name use is the type checker's own record (Uses, Selections), judged on the
// member's own line. Every use that sees all of a type is read here: a value of another package's named type copied,
// sized, compared or converted, a value of it or a pointer to it handed to an interface, a type argument, a constraint,
// a field, parameter or variable holding it by value, a map keyed by it, an embedding of it. The rule of thumb is the
// design's: a use is a member use only when the package reaches the type through a selector, a key or a name, and
// anything that moves a value of the type, or a pointer to it, into a place that sees all of it is a whole use.
//
// Every value expression the walk cannot place is taken as moved to a place it cannot see, which records the most a
// move can need (useMoved with no destination), and every type expression it cannot place as a declaration holding
// the type by value. A use recorded wider than it is costs a package a check from source in another build; one
// recorded narrower restores a stale analysis, so every doubt goes wide.

// useRole is what the expression's parent does with it, which decides the kind of use its type gets.
type useRole string

const (
	// roleMoved is a value assigned, passed, returned, sent or stored in a literal, to destination when known.
	roleMoved useRole = "Moved"

	// roleExempt is a value the parent neither copies nor inspects whole: the operand of a selector, of `&`, of `*`,
	// an assignment's target, a call's discarded result, an asserted interface.
	roleExempt useRole = "Exempt"

	// roleUnderlying is a value an operation on its underlying type reads: arithmetic, `len`, an index, a slice, a
	// receive, a call of a value of a named function type.
	roleUnderlying useRole = "Underlying"

	// roleCompared is a value `==`, `!=` or a switch compares, against a value of other.
	roleCompared useRole = "Compared"

	// roleSized is a value whose size, alignment or offsets `unsafe` reads.
	roleSized useRole = "Sized"

	// roleConverted is a value converted to destination.
	roleConverted useRole = "Converted"

	// roleWhole is a value a rule reads the whole type of past what the type checker records: the value a struct
	// literal copies fields from, which staticcheck's S1016 judges convertible to the literal's type.
	roleWhole useRole = "Whole"

	// roleTypeArgument is a type argument written, which the instance's own record judges (useWalk.instance).
	roleTypeArgument useRole = "TypeArgument"

	// roleDeclared is a type expression that declares a field, parameter, result, variable or type: what it holds by
	// value is used whole.
	roleDeclared useRole = "Declared"

	// roleComponent is a type expression inside another, which the outer one's role judges.
	roleComponent useRole = "Component"

	// roleConstraint is a type parameter's constraint, every type it reaches used whole.
	roleConstraint useRole = "Constraint"

	// roleEmbeddedField is the type of a field a struct of the package embeds, by value or by pointer.
	roleEmbeddedField useRole = "EmbeddedField"

	// roleEmbeddedInterface is a type an interface of the package embeds.
	roleEmbeddedInterface useRole = "EmbeddedInterface"

	// roleMapKey is a map's key type, which must be comparable.
	roleMapKey useRole = "MapKey"

	// roleConversion is the type a conversion converts to.
	roleConversion useRole = "Conversion"

	// roleAsserted is the type a type assertion or a type switch's case asserts.
	roleAsserted useRole = "Asserted"
)

// useContext is what the walk knows of where an expression sits: its role, the type a moved or converted value goes
// to, nil when not known, the types a tuple's values go to, one each, and the type a compared value is compared with.
type useContext struct {
	role         useRole
	destination  types.Type
	destinations []types.Type
	other        types.Type
}

// useWalk is one package's walk of its syntax for its kinds of use: the package, its type information, what it has
// collected, and the context each expression's parent set before the walk reached it.
type useWalk struct {
	own       *types.Package
	info      *types.Info
	collected *typeReads
	contexts  map[ast.Expr]useContext

	// emptyInterface is the universe's any, the destination of a value handed to panic.
	emptyInterface types.Type

	// seen are the named types of the package's own a walk into them has entered, so a recursive type ends it.
	seen map[*types.Named]bool

	// wholeOperands are the selectors whose operand a rule reads whole (roleWhole), marked by the literal that holds
	// them before the walk reaches them.
	wholeOperands map[*ast.SelectorExpr]bool
}

// walkUses records every kind of use files make of other packages' types, by where each expression sits, and every
// instance's type arguments.
//
// Mutates: collected gains each use
func walkUses(collected *typeReads, own *types.Package, info *types.Info, files []*ast.File) {
	walk := &useWalk{own: own, info: info, collected: collected, contexts: map[ast.Expr]useContext{},
		emptyInterface: types.Universe.Lookup("any").Type(), seen: map[*types.Named]bool{},
		wholeOperands: map[*ast.SelectorExpr]bool{}}
	for _, file := range files {
		for _, declaration := range file.Decls {
			var results *types.Tuple
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				if declared, isFunction := info.Defs[function.Name].(*types.Func); isFunction {
					results = declared.Signature().Results()
				}
			}
			walk.inspect(declaration, results)
		}
	}
	for identifier, instance := range info.Instances {
		walk.instance(identifier, instance)
	}
}

// inspect walks root, a declaration or a function literal, whose return statements return results, each function
// literal within it walked with its own.
func (walk *useWalk) inspect(root ast.Node, results *types.Tuple) {
	ast.Inspect(root, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.FuncLit); isLiteral && node != root {
			// The literal's own value is placed before its body is walked with its own results.
			walk.apply(literal)
			var literalResults *types.Tuple
			if signature, isSignature := walk.info.TypeOf(literal).(*types.Signature); isSignature {
				literalResults = signature.Results()
			}
			walk.inspect(literal.Type, nil)
			walk.inspect(literal.Body, literalResults)
			return false
		}
		walk.place(node, results)
		if expression, isExpression := node.(ast.Expr); isExpression {
			walk.apply(expression)
		}
		return true
	})
}

// set gives expression role, where the walk will find it when it reaches the expression, after its parent.
func (walk *useWalk) set(expression ast.Expr, role useRole) {
	walk.contexts[expression] = useContext{role: role}
}

// moved places expression as a value moved to destination, nil when not known.
func (walk *useWalk) moved(expression ast.Expr, destination types.Type) {
	walk.contexts[expression] = useContext{role: roleMoved, destination: destination}
}

// isType reports whether expression is a type expression.
func (walk *useWalk) isType(expression ast.Expr) bool {
	typeAndValue, recorded := walk.info.Types[expression]
	return recorded && typeAndValue.IsType()
}

// place sets the context of each child of node whose role node decides.
func (walk *useWalk) place(node ast.Node, results *types.Tuple) {
	switch node := node.(type) {
	case *ast.ParenExpr:
		walk.contexts[node.X] = walk.contexts[node]
	case *ast.SelectorExpr:
		// A qualified identifier's operand is a package name, which no expression type holds.
		if _, selected := walk.info.Selections[node]; selected {
			if walk.wholeOperands[node] {
				walk.set(node.X, roleWhole)
			} else {
				walk.set(node.X, roleExempt)
			}
		}
	case *ast.StarExpr:
		if walk.isType(node) {
			walk.set(node.X, roleComponent)
		} else {
			walk.set(node.X, roleExempt)
		}
	case *ast.UnaryExpr:
		if node.Op == token.AND {
			walk.set(node.X, roleExempt)
		} else {
			walk.set(node.X, roleUnderlying)
		}
	case *ast.BinaryExpr:
		if node.Op == token.EQL || node.Op == token.NEQ {
			walk.contexts[node.X] = useContext{role: roleCompared, other: walk.info.TypeOf(node.Y)}
			walk.contexts[node.Y] = useContext{role: roleCompared, other: walk.info.TypeOf(node.X)}
		} else {
			walk.set(node.X, roleUnderlying)
			walk.set(node.Y, roleUnderlying)
		}
	case *ast.IndexExpr:
		walk.placeIndex(node, node.X, []ast.Expr{node.Index})
	case *ast.IndexListExpr:
		walk.placeIndex(node, node.X, node.Indices)
	case *ast.SliceExpr:
		for _, operand := range []ast.Expr{node.X, node.Low, node.High, node.Max} {
			if operand != nil {
				walk.set(operand, roleUnderlying)
			}
		}
	case *ast.TypeAssertExpr:
		walk.set(node.X, roleExempt)
		if node.Type != nil {
			walk.set(node.Type, roleAsserted)
		}
	case *ast.CallExpr:
		walk.placeCall(node)
	case *ast.CompositeLit:
		walk.placeComposite(node)
	case *ast.AssignStmt:
		walk.placeAssignment(node)
	case *ast.ValueSpec:
		walk.placeValueSpec(node)
	case *ast.ReturnStmt:
		walk.placeReturn(node, results)
	case *ast.SendStmt:
		walk.set(node.Chan, roleUnderlying)
		var element types.Type
		if channel, isChannel := underlyingOf(walk.info.TypeOf(node.Chan)).(*types.Chan); isChannel {
			element = channel.Elem()
		}
		walk.moved(node.Value, element)
	case *ast.IncDecStmt:
		walk.set(node.X, roleUnderlying)
	case *ast.RangeStmt:
		walk.placeRange(node)
	case *ast.SwitchStmt:
		walk.placeSwitch(node)
	case *ast.TypeSwitchStmt:
		for _, clause := range node.Body.List {
			if caseClause, isCase := clause.(*ast.CaseClause); isCase {
				for _, asserted := range caseClause.List {
					walk.set(asserted, roleAsserted)
				}
			}
		}
	case *ast.ExprStmt:
		walk.set(node.X, roleExempt)
	case *ast.GoStmt:
		walk.set(node.Call, roleExempt)
	case *ast.DeferStmt:
		walk.set(node.Call, roleExempt)
	case *ast.TypeSpec:
		walk.set(node.Type, roleDeclared)
		walk.placeFields(node.TypeParams, roleConstraint)
	case *ast.FuncType:
		walk.placeFields(node.TypeParams, roleConstraint)
		walk.placeFields(node.Params, roleDeclared)
		walk.placeFields(node.Results, roleDeclared)
	case *ast.StructType:
		for _, field := range node.Fields.List {
			if len(field.Names) == 0 {
				walk.set(field.Type, roleEmbeddedField)
			} else {
				walk.set(field.Type, roleDeclared)
			}
		}
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			if len(field.Names) == 0 {
				walk.set(field.Type, roleEmbeddedInterface)
			} else {
				walk.set(field.Type, roleComponent)
			}
		}
	case *ast.ArrayType:
		walk.set(node.Elt, roleComponent)
	case *ast.MapType:
		walk.set(node.Key, roleMapKey)
		walk.set(node.Value, roleComponent)
	case *ast.ChanType:
		walk.set(node.Value, roleComponent)
	case *ast.Ellipsis:
		if node.Elt != nil {
			walk.set(node.Elt, roleComponent)
		}
	}
}

// placeFields gives each field of fields, nil for none, role.
func (walk *useWalk) placeFields(fields *ast.FieldList, role useRole) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		walk.set(field.Type, role)
	}
}

// placeIndex places an index expression's operand and indices: an instantiation's type arguments, judged by the
// instance's record, or an index of a value, a map's key moved to the key's type.
func (walk *useWalk) placeIndex(node ast.Expr, operand ast.Expr, indices []ast.Expr) {
	if walk.isType(node) || len(indices) > 0 && walk.isType(indices[0]) {
		walk.set(operand, roleComponent)
		for _, index := range indices {
			walk.set(index, roleTypeArgument)
		}
		return
	}
	walk.set(operand, roleUnderlying)
	if mapType, isMap := underlyingOf(walk.info.TypeOf(operand)).(*types.Map); isMap {
		walk.moved(indices[0], mapType.Key())
		return
	}
	walk.set(indices[0], roleUnderlying)
}

// placeCall places a call's function and arguments: a conversion's operand, a builtin's arguments by the builtin, and
// an ordinary call's arguments moved to its parameters.
func (walk *useWalk) placeCall(call *ast.CallExpr) {
	function := ast.Unparen(call.Fun)
	if walk.isType(function) {
		walk.set(call.Fun, roleConversion)
		if len(call.Args) == 1 {
			walk.contexts[call.Args[0]] = useContext{role: roleConverted, destination: walk.info.TypeOf(call.Fun)}
		}
		return
	}
	if builtin := walk.builtinOf(function); builtin != nil {
		walk.placeBuiltin(call, builtin.Name())
		return
	}
	walk.set(call.Fun, roleUnderlying)
	signature, isSignature := underlyingOf(walk.info.TypeOf(call.Fun)).(*types.Signature)
	if !isSignature {
		// A call of a value whose type is a type parameter: its arguments keep no destination, which goes wide.
		return
	}
	parameters := signature.Params()
	var destinations []types.Type
	for index := range parameters.Len() {
		destinations = append(destinations, parameters.At(index).Type())
	}
	if _, isTuple := walk.info.TypeOf(firstOf(call.Args)).(*types.Tuple); isTuple {
		// One call's results spread over the parameters.
		walk.contexts[call.Args[0]] = useContext{role: roleMoved, destinations: destinations}
		return
	}
	for index, argument := range call.Args {
		var destination types.Type
		switch {
		case signature.Variadic() && index >= parameters.Len()-1:
			last := parameters.At(parameters.Len() - 1).Type()
			if call.Ellipsis.IsValid() {
				destination = last
			} else if slice, isSlice := underlyingOf(last).(*types.Slice); isSlice {
				destination = slice.Elem()
			}
		case index < parameters.Len():
			destination = parameters.At(index).Type()
		}
		walk.moved(argument, destination)
	}
}

// firstOf is the first of expressions, nil for none.
func firstOf(expressions []ast.Expr) ast.Expr {
	if len(expressions) == 0 {
		return nil
	}
	return expressions[0]
}

// builtinOf is the builtin function, the universe's or unsafe's, that function names, nil for any other.
func (walk *useWalk) builtinOf(function ast.Expr) *types.Builtin {
	var identifier *ast.Ident
	switch named := function.(type) {
	case *ast.Ident:
		identifier = named
	case *ast.SelectorExpr:
		identifier = named.Sel
	default:
		return nil
	}
	builtin, isBuiltin := walk.info.Uses[identifier].(*types.Builtin)
	if !isBuiltin {
		return nil
	}
	return builtin
}

// placeBuiltin places the arguments of a call of the builtin named name. A builtin the walk does not name leaves its
// arguments without a context, which goes wide.
func (walk *useWalk) placeBuiltin(call *ast.CallExpr, name string) {
	walk.set(call.Fun, roleExempt)
	switch name {
	case "len", "cap", "clear", "close", "copy", "complex", "real", "imag", "min", "max", "Add", "Slice", "SliceData",
		"String", "StringData":
		for _, argument := range call.Args {
			walk.set(argument, roleUnderlying)
		}
	case "append":
		if len(call.Args) == 0 {
			return
		}
		walk.set(call.Args[0], roleUnderlying)
		var element types.Type
		if slice, isSlice := underlyingOf(walk.info.TypeOf(call.Args[0])).(*types.Slice); isSlice {
			element = slice.Elem()
		}
		for _, argument := range call.Args[1:] {
			if call.Ellipsis.IsValid() {
				walk.set(argument, roleUnderlying)
			} else {
				walk.moved(argument, element)
			}
		}
	case "delete":
		if len(call.Args) != 2 {
			return
		}
		walk.set(call.Args[0], roleUnderlying)
		var key types.Type
		if mapType, isMap := underlyingOf(walk.info.TypeOf(call.Args[0])).(*types.Map); isMap {
			key = mapType.Key()
		}
		walk.moved(call.Args[1], key)
	case "make", "new":
		for index, argument := range call.Args {
			if index == 0 {
				walk.set(argument, roleDeclared)
			} else {
				walk.set(argument, roleUnderlying)
			}
		}
	case "panic":
		for _, argument := range call.Args {
			walk.moved(argument, walk.emptyInterface)
		}
	case "Sizeof", "Alignof":
		for _, argument := range call.Args {
			walk.set(argument, roleSized)
		}
	case "Offsetof":
		// The offset of a field is read from the layout of the struct it is selected from.
		for _, argument := range call.Args {
			if selector, isSelector := ast.Unparen(argument).(*ast.SelectorExpr); isSelector {
				walk.set(argument, roleExempt)
				walk.set(selector.X, roleSized)
			}
		}
	}
}

// placeComposite places a composite literal's elements, each moved to the field, element or key type it fills. A
// struct literal with elements is a use of its type whole, recorded here, since an elided literal type has no type
// expression of its own: an unkeyed one reads every field in order, and a keyed one is counted against the type's
// fields by staticcheck's S1016, which also reads whole the type of each value its elements select a field of.
func (walk *useWalk) placeComposite(literal *ast.CompositeLit) {
	if literal.Type != nil {
		walk.set(literal.Type, roleComponent)
	}
	literalType := walk.info.TypeOf(literal)
	if pointer, isPointer := underlyingOf(literalType).(*types.Pointer); isPointer {
		literalType = pointer.Elem()
	}
	switch composite := underlyingOf(literalType).(type) {
	case *types.Struct:
		for index, element := range literal.Elts {
			value := element
			if pair, isPair := element.(*ast.KeyValueExpr); isPair {
				// A key names a field, a use the type checker records, and moves nothing.
				walk.set(pair.Key, roleExempt)
				var fieldType types.Type
				if key, isIdentifier := pair.Key.(*ast.Ident); isIdentifier {
					if field, isField := walk.info.Uses[key].(*types.Var); isField {
						fieldType = field.Type()
					}
				}
				walk.moved(pair.Value, fieldType)
				value = pair.Value
			} else if index < composite.NumFields() {
				walk.moved(element, composite.Field(index).Type())
			}
			if selector, isSelector := ast.Unparen(value).(*ast.SelectorExpr); isSelector {
				if _, selected := walk.info.Selections[selector]; selected {
					walk.wholeOperands[selector] = true
				}
			}
		}
		if len(literal.Elts) > 0 {
			walk.byValue(literalType)
		}
	case *types.Array:
		walk.placeElements(literal.Elts, nil, composite.Elem())
	case *types.Slice:
		walk.placeElements(literal.Elts, nil, composite.Elem())
	case *types.Map:
		walk.placeElements(literal.Elts, composite.Key(), composite.Elem())
	}
}

// placeElements places an array, slice or map literal's elements, each value moved to element, and each key moved to
// key, or, nil for an array or slice, read as an index.
func (walk *useWalk) placeElements(elements []ast.Expr, key types.Type, element types.Type) {
	for _, each := range elements {
		pair, isPair := each.(*ast.KeyValueExpr)
		if !isPair {
			walk.moved(each, element)
			continue
		}
		if key == nil {
			walk.set(pair.Key, roleUnderlying)
		} else {
			walk.moved(pair.Key, key)
		}
		walk.moved(pair.Value, element)
	}
}

// placeAssignment places an assignment's targets, which nothing reads, and its values, each moved to its target's
// type, or, for an assignment that operates (`+=`), read as an operand of the operation.
func (walk *useWalk) placeAssignment(assignment *ast.AssignStmt) {
	if assignment.Tok != token.ASSIGN && assignment.Tok != token.DEFINE {
		for _, operand := range append(append([]ast.Expr{}, assignment.Lhs...), assignment.Rhs...) {
			walk.set(operand, roleUnderlying)
		}
		return
	}
	var destinations []types.Type
	for _, target := range assignment.Lhs {
		walk.set(target, roleExempt)
		destinations = append(destinations, walk.info.TypeOf(target))
	}
	walk.placeValues(assignment.Rhs, destinations)
}

// placeValueSpec places a variable or constant declaration's type, which declares, and its values, each moved to the
// declared type, or to its name's.
func (walk *useWalk) placeValueSpec(specification *ast.ValueSpec) {
	if specification.Type != nil {
		walk.set(specification.Type, roleDeclared)
	}
	destinations := make([]types.Type, 0, len(specification.Names))
	for _, name := range specification.Names {
		destinations = append(destinations, walk.info.TypeOf(name))
	}
	walk.placeValues(specification.Values, destinations)
}

// placeReturn places a return's values, each moved to its result's type.
func (walk *useWalk) placeReturn(statement *ast.ReturnStmt, results *types.Tuple) {
	if results == nil {
		return
	}
	destinations := make([]types.Type, 0, results.Len())
	for index := range results.Len() {
		destinations = append(destinations, results.At(index).Type())
	}
	walk.placeValues(statement.Results, destinations)
}

// placeValues places values moved to destinations, one each, or, for one value of many results, all of them to it.
func (walk *useWalk) placeValues(values []ast.Expr, destinations []types.Type) {
	if len(values) == 1 && len(destinations) > 1 {
		walk.contexts[values[0]] = useContext{role: roleMoved, destinations: destinations}
		return
	}
	for index, value := range values {
		var destination types.Type
		if index < len(destinations) {
			destination = destinations[index]
		}
		walk.moved(value, destination)
	}
}

// placeRange places a range's operand, read through its underlying type, and records its key and value variables,
// each a copy of what it ranges over.
func (walk *useWalk) placeRange(statement *ast.RangeStmt) {
	walk.set(statement.X, roleUnderlying)
	for _, variable := range []ast.Expr{statement.Key, statement.Value} {
		if variable == nil {
			continue
		}
		walk.set(variable, roleExempt)
		if copied := walk.info.TypeOf(variable); copied != nil {
			walk.byValue(copied)
		}
	}
}

// placeSwitch places a switch's tag and its cases' values, each compared with the other, or, for a switch with no
// tag, read as conditions.
func (walk *useWalk) placeSwitch(statement *ast.SwitchStmt) {
	if statement.Tag == nil {
		for _, clause := range statement.Body.List {
			if caseClause, isCase := clause.(*ast.CaseClause); isCase {
				for _, condition := range caseClause.List {
					walk.set(condition, roleUnderlying)
				}
			}
		}
		return
	}
	tagType := walk.info.TypeOf(statement.Tag)
	other := tagType
	for _, clause := range statement.Body.List {
		caseClause, isCase := clause.(*ast.CaseClause)
		if !isCase {
			continue
		}
		for _, value := range caseClause.List {
			walk.contexts[value] = useContext{role: roleCompared, other: tagType}
			if valueType := walk.info.TypeOf(value); valueType != nil && types.IsInterface(valueType) {
				other = valueType
			}
		}
	}
	walk.contexts[statement.Tag] = useContext{role: roleCompared, other: other}
}

// apply records the uses expression's type gets where it sits.
func (walk *useWalk) apply(expression ast.Expr) {
	typeAndValue, recorded := walk.info.Types[expression]
	if !recorded || typeAndValue.Type == nil {
		return
	}
	context := walk.contexts[expression]
	if typeAndValue.IsType() {
		walk.typeExpression(typeAndValue.Type, context)
		return
	}
	if typeAndValue.IsVoid() || typeAndValue.IsBuiltin() || typeAndValue.IsNil() {
		return
	}
	walk.value(typeAndValue.Type, context)
}

// typeExpression records the uses a type expression of type written makes where it sits. One the walk cannot place
// is taken as declaring a value of the type.
func (walk *useWalk) typeExpression(written types.Type, context useContext) {
	switch context.role {
	case roleComponent, roleTypeArgument, roleExempt:
	case roleConstraint:
		walk.deep(written)
	case roleEmbeddedField, roleEmbeddedInterface:
		// An embedded type's fields and methods become the package's own type's, with its size and method set, and
		// a rule reading the package's type reads them there, through a pointer too: unused_code's graph_types reads
		// another package's struct through the fields the package's own structs embed, and vet's structtag reads the
		// tags of the exported fields of each struct embedded. So an embedding, by pointer as by value, is a use of the
		// embedded type whole, as the design's table rules, never only of its method set.
		walk.deep(written)
	case roleMapKey:
		walk.byValue(written)
	case roleConversion:
		walk.byValue(written)
		walk.pointedToByValue(written)
	case roleAsserted:
		if named := walk.foreign(written); named != nil {
			walk.use(named, UseMethodSet)
			return
		}
		walk.deep(written)
	default:
		walk.byValue(written)
	}
}

// value records the uses a value of type valueType makes where it sits. One the walk cannot place is taken as moved
// to a place it cannot see.
func (walk *useWalk) value(valueType types.Type, context useContext) {
	if tuple, isTuple := valueType.(*types.Tuple); isTuple {
		for index := range tuple.Len() {
			var destination types.Type
			if index < len(context.destinations) {
				destination = context.destinations[index]
			}
			walk.movedTo(tuple.At(index).Type(), destination)
		}
		return
	}
	switch context.role {
	case roleExempt, roleTypeArgument, roleComponent:
	case roleUnderlying:
		walk.underlying(valueType)
	case roleCompared:
		walk.compared(valueType, context.other)
	case roleSized, roleWhole:
		walk.byValue(valueType)
		walk.pointedToByValue(valueType)
	case roleConverted:
		walk.movedTo(valueType, context.destination)
		walk.pointedToByValue(valueType)
	default:
		destination := context.destination
		if len(context.destinations) > 0 {
			// A value of one result, comma-ok's, goes to the first target.
			destination = context.destinations[0]
		}
		walk.movedTo(valueType, destination)
	}
}

// movedTo records the uses a value of type moved makes going to destination, nil when the walk cannot tell where: a
// copy of everything it holds by value, and, handed to an interface, the method set the interface asks, or, to an
// empty interface, which any rule may look through, everything a look through it can reach. A destination the walk
// cannot see may be an empty interface.
//
// Every move copies, a fresh composite literal's or call's result too: copylocks lets those two pass, but no rule has
// been shown to read nothing more of a fresh value than of a copied one, so telling them apart would narrow the read
// on no proof. A non-empty interface is its method set alone because no rule that walks a type's fields walks a
// value handed to one, which go_engine's fieldWalkRows prove by their non-empty interface row: a rule that did would
// make these moves whole.
func (walk *useWalk) movedTo(moved types.Type, destination types.Type) {
	walk.byValue(moved)
	if destination == nil {
		walk.reachable(moved)
		return
	}
	if _, isParameter := destination.(*types.TypeParam); isParameter || !types.IsInterface(destination) {
		// A type parameter's argument is the instance's to judge (useWalk.instance).
		return
	}
	if _, isParameter := moved.(*types.TypeParam); isParameter {
		return
	}
	interfaceType, isInterface := destination.Underlying().(*types.Interface)
	if isInterface && interfaceType.Empty() && !types.IsInterface(moved) {
		walk.reachable(moved)
		return
	}
	walk.methodSet(moved)
	walk.methodSet(destination)
}

// compared records the uses a value of type compared makes compared with a value of other: its comparability, unless
// it is a pointer or an interface, which compare by identity, and its conversion to other when other is an interface.
func (walk *useWalk) compared(compared types.Type, other types.Type) {
	if !pointerType(compared) && !types.IsInterface(compared) {
		walk.byValue(compared)
	}
	if other != nil && types.IsInterface(other) && !types.IsInterface(compared) {
		walk.movedTo(compared, other)
	}
}

// underlying records the operated value's named type of another package, or the one a pointer it is points to, as a
// use of its underlying type.
func (walk *useWalk) underlying(operated types.Type) {
	if named := walk.foreign(operated); named != nil {
		walk.use(named, UseUnderlying)
	}
}

// methodSet records the named type of another package that typed is, or points to, as a use of its method set.
func (walk *useWalk) methodSet(typed types.Type) {
	if named := walk.foreign(typed); named != nil {
		walk.use(named, UseMethodSet)
	}
}

// byValue records each named type of another package held is or holds by value, through the fields of an unnamed
// struct, the elements of an array, and the type arguments of an instance, as a use of the type whole: its size,
// comparability and every field a rule may recurse into are read with the value. A named interface held so is its
// name alone, since a copy of an interface value reads nothing of the interface. A named type of the package's own
// holds what its declaration declares, which the declaration's own walk records.
func (walk *useWalk) byValue(held types.Type) {
	switch held := types.Unalias(held).(type) {
	case *types.Named:
		if walk.isForeign(held) && !types.IsInterface(held) {
			walk.use(held, UseWhole)
		}
		for argument := range held.TypeArgs().Types() {
			walk.byValue(argument)
		}
	case *types.Struct:
		for field := range held.Fields() {
			walk.byValue(field.Type())
		}
	case *types.Array:
		walk.byValue(held.Elem())
	}
}

// pointedToByValue records what a pointer type pointed holds by value at the other end, which a conversion between
// pointer types and an `unsafe` read of one see.
func (walk *useWalk) pointedToByValue(pointed types.Type) {
	if pointer, isPointer := types.Unalias(pointed).Underlying().(*types.Pointer); isPointer {
		walk.byValue(pointer.Elem())
	}
}

// reachable records each named type of another package a look through a value of type looked can reach, as a use of
// the type whole: through fields, elements, keys and pointers at any depth, and through the declarations of the
// package's own named types, as printf's matching and a marshaller's walk go, and never into a function or a channel,
// which no such look enters. printf looks through a pointer only at the top, but a marshaller follows every one, so
// every pointer is followed. A named interface of another package is its method set.
func (walk *useWalk) reachable(looked types.Type) {
	switch looked := types.Unalias(looked).(type) {
	case *types.Named:
		if walk.isForeign(looked) {
			if types.IsInterface(looked) {
				walk.use(looked, UseMethodSet)
			} else {
				walk.use(looked, UseWhole)
			}
			for argument := range looked.TypeArgs().Types() {
				walk.reachable(argument)
			}
			return
		}
		if walk.seen[looked] {
			return
		}
		walk.seen[looked] = true
		walk.reachable(looked.Underlying())
		for argument := range looked.TypeArgs().Types() {
			walk.reachable(argument)
		}
	case *types.Pointer:
		walk.reachable(looked.Elem())
	case *types.Struct:
		for field := range looked.Fields() {
			walk.reachable(field.Type())
		}
	case *types.Array:
		walk.reachable(looked.Elem())
	case *types.Slice:
		walk.reachable(looked.Elem())
	case *types.Map:
		walk.reachable(looked.Key())
		walk.reachable(looked.Elem())
	}
}

// deep records every named type of another package reached may reach, through every kind of type, signatures and
// interfaces included, as a use of the type whole: what a constraint, or an unnamed type asserted, asks of a type is
// any of it.
func (walk *useWalk) deep(reached types.Type) {
	switch reached := types.Unalias(reached).(type) {
	case *types.Named:
		if walk.isForeign(reached) {
			walk.use(reached, UseWhole)
		}
		for argument := range reached.TypeArgs().Types() {
			walk.deep(argument)
		}
	case *types.Pointer:
		walk.deep(reached.Elem())
	case *types.Slice:
		walk.deep(reached.Elem())
	case *types.Array:
		walk.deep(reached.Elem())
	case *types.Map:
		walk.deep(reached.Key())
		walk.deep(reached.Elem())
	case *types.Chan:
		walk.deep(reached.Elem())
	case *types.Struct:
		for field := range reached.Fields() {
			walk.deep(field.Type())
		}
	case *types.Signature:
		for parameter := range reached.Params().Variables() {
			walk.deep(parameter.Type())
		}
		for result := range reached.Results().Variables() {
			walk.deep(result.Type())
		}
	case *types.Interface:
		for embedded := range reached.EmbeddedTypes() {
			walk.deep(embedded)
		}
		for method := range reached.ExplicitMethods() {
			walk.deep(method.Type())
		}
	case *types.Union:
		for index := range reached.Len() {
			walk.deep(reached.Term(index).Type())
		}
	}
}

// instance records what instantiating a generic reads: each type argument by what a constraint can ask of it, and,
// for another package's generic, every type its type parameters' constraints reach.
func (walk *useWalk) instance(identifier *ast.Ident, instance types.Instance) {
	for argument := range instance.TypeArgs.Types() {
		walk.typeArgument(argument)
	}
	generic := walk.info.Uses[identifier]
	if generic == nil || generic.Pkg() == nil || generic.Pkg() == walk.own {
		// The package's own generic's constraints are its own declaration's, which its walk records.
		return
	}
	var parameters *types.TypeParamList
	switch generic := generic.(type) {
	case *types.Func:
		parameters = generic.Origin().Signature().TypeParams()
	case *types.TypeName:
		if named, isNamed := types.Unalias(generic.Type()).(*types.Named); isNamed {
			parameters = named.TypeParams()
		}
	}
	if parameters == nil {
		return
	}
	for parameter := range parameters.TypeParams() {
		walk.deep(parameter.Constraint())
	}
}

// typeArgument records what a constraint can ask of argument, a type argument: of a named type of another package, all
// of it, or, for an interface, its method set; of a pointer to one, the method set, since a pointer is comparable and
// its own core type; of an unnamed struct or array, its comparability through what it holds by value; and of an
// unnamed interface, everything it reaches.
func (walk *useWalk) typeArgument(argument types.Type) {
	switch argument := types.Unalias(argument).(type) {
	case *types.Named:
		if walk.isForeign(argument) {
			if types.IsInterface(argument) {
				walk.use(argument, UseMethodSet)
			} else {
				walk.use(argument, UseWhole)
			}
		}
		for nested := range argument.TypeArgs().Types() {
			walk.typeArgument(nested)
		}
	case *types.Pointer:
		walk.methodSet(argument)
	case *types.Struct, *types.Array:
		walk.byValue(argument)
	case *types.Interface:
		walk.deep(argument)
	}
}

// foreign is the named type of another package typed is, or points to, nil for any other type.
func (walk *useWalk) foreign(typed types.Type) *types.Named {
	if typed == nil {
		return nil
	}
	return foreignNamed(typed, walk.own)
}

// isForeign reports whether named is declared by another package than the walk's.
func (walk *useWalk) isForeign(named *types.Named) bool {
	return named.Obj().Pkg() != nil && named.Obj().Pkg() != walk.own
}

// use records a use of kind of named, as its generic declares it.
func (walk *useWalk) use(named *types.Named, kind UseKind) {
	walk.collected.add(walk.own, named.Origin().Obj(), kind)
}

// pointerType reports whether typed is a pointer type.
func pointerType(typed types.Type) bool {
	_, pointer := types.Unalias(typed).Underlying().(*types.Pointer)
	return pointer
}

// underlyingOf is typed's underlying type, nil for nil.
func underlyingOf(typed types.Type) types.Type {
	if typed == nil {
		return nil
	}
	return typed.Underlying()
}
