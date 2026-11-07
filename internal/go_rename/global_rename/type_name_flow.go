package global_rename

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// What reads a type's name at run time, as the encoding guard asks it of a type: a json, gob or yaml encoder reads
// a struct's field names and never its type's name, so reaching one of those keeps a field's name, not a type's.
// A type's name is read by the reflect.Type of a value of it reaching Name, String or PkgPath, or the
// reflect.Value of it reaching String or Interface, or either leaving for code the workspace does not hold (fmt's
// %v of a reflect.Type is its String); by fmt's %T or %#v of a value of it; by gob.Register, which sends it as
// the name of an interface's concrete type; and, kept as before, by xml, which names an element for its type, and
// by a template, which can print %T. typeNameFlow follows the reflect half forward; formatTypeNameOperands reads
// the fmt half off the format string.

// reflectTypeNameMethods are the methods that read a reflect value's type name: reflect.Type's Name, String and
// PkgPath, and reflect.Value's String (`<T Value>`) and Interface, which hands the value back to code that may
// print it with %T.
var reflectTypeNameMethods = map[string]map[string]bool{
	"Type":  {"Name": true, "String": true, "PkgPath": true},
	"Value": {"String": true, "Interface": true},
}

// reflectSameTypeMethods are the reflect methods whose result is, or spells, the type of the value they are
// called on: Value.Type, Value.Addr, and Elem, which for a pointer is what it points to. Every other method that
// returns a reflect value (Field, Key, In, Out, Index, MapIndex) returns one of a type the first type holds.
var reflectSameTypeMethods = map[string]bool{"Type": true, "Addr": true, "Elem": true}

// nameBlindPackages are the libraries whose functions never read the name of what they are handed: they sort,
// search and collect. A reflect value handed to any other library leaves the analysis, and is taken as read.
var nameBlindPackages = map[string]bool{"sort": true, "slices": true, "maps": true, "iter": true}

// flowMode is whether a reflect value is the type a value of the origin's type has (own), or a type it holds,
// reached through Field, Key, Index or the like (held).
type flowMode uint8

const (
	flowOwn flowMode = 1 << iota
	flowHeld
)

// typeNameFlow follows each reflect value made from a Go value (reflect.TypeOf, reflect.ValueOf, reflect.TypeFor)
// forward through the workspace: through variables, fields of local values, containers, the parameters and
// results of workspace functions, to what reads its type's name, or out of the workspace's sight. A value stored
// through a parameter, returned from a method or a function literal, or handed to a function value is out of
// sight. It is field- and container-insensitive: a local holding a reflect value anywhere in it is taken to hold
// it everywhere.
type typeNameFlow struct {
	analysis *encodingAnalysis
	indexed  bool
	// uses holds every use of a variable whose type can hold a reflect value, and of a function whose results can.
	uses map[Key][]flowSite
	// declarations holds each workspace function's declaration, by its key.
	declarations map[Key]flowDeclaration
	// parameters holds every parameter, receiver and named result, by its key, with its function.
	parameters map[Key]flowParameter
	parents    map[*ast.File]map[ast.Node]ast.Node
	carrying   map[types.Type]bool
	answers    map[*ast.CallExpr]encoder
}

// flowSite is one expression in one file of one package variant.
type flowSite struct {
	goPackage  *packages.Package
	file       *ast.File
	expression ast.Expr
}

type flowDeclaration struct {
	goPackage   *packages.Package
	declaration *ast.FuncDecl
}

type flowParameter struct {
	goPackage *packages.Package
	function  ast.Node
	result    bool
}

type flowItem struct {
	site flowSite
	mode flowMode
}

func newTypeNameFlow(analysis *encodingAnalysis) *typeNameFlow {
	return &typeNameFlow{
		analysis:     analysis,
		uses:         map[Key][]flowSite{},
		declarations: map[Key]flowDeclaration{},
		parameters:   map[Key]flowParameter{},
		parents:      map[*ast.File]map[ast.Node]ast.Node{},
		carrying:     map[types.Type]bool{},
		answers:      map[*ast.CallExpr]encoder{},
	}
}

// reads is what the reflect value a call makes reads: encoderReflectTypeName when the name of its own type may
// be read, encoderHeldTypeName when only the names of the types it holds may be, else zero.
func (flow *typeNameFlow) reads(goPackage *packages.Package, call *ast.CallExpr) encoder {
	if answer, known := flow.answers[call]; known {
		return answer
	}
	flow.index()
	file := fileHolding(goPackage, call.Pos())
	answer := encoder(0)
	if file == nil {
		answer = encoderReflectTypeName
	} else {
		state := &flowState{
			seenObjects: map[Key]flowMode{}, seenResults: map[Key]flowMode{}, seenSites: map[ast.Expr]flowMode{},
		}
		flow.push(state, flowSite{goPackage: goPackage, file: file, expression: call}, flowOwn)
		for len(state.pending) > 0 && !state.ownRead {
			item := state.pending[len(state.pending)-1]
			state.pending = state.pending[:len(state.pending)-1]
			flow.follow(state, item)
		}
		switch {
		case state.ownRead:
			answer = encoderReflectTypeName
		case state.heldRead:
			answer = encoderHeldTypeName
		}
	}
	flow.answers[call] = answer
	return answer
}

// fileHolding is the file of a package variant that holds a position.
func fileHolding(goPackage *packages.Package, position token.Pos) *ast.File {
	for _, file := range goPackage.Syntax {
		if file.FileStart <= position && position <= file.FileEnd {
			return file
		}
	}
	return nil
}

// index records, once, every declaration, parameter and use the walks need.
func (flow *typeNameFlow) index() {
	if flow.indexed {
		return
	}
	flow.indexed = true
	keyer := flow.analysis.engine.keyer
	for _, analyzedPackage := range flow.analysis.engine.workspace.Analyzed {
		info := analyzedPackage.TypesInfo
		recordParameters := func(function ast.Node, fields *ast.FieldList, result bool) {
			if fields == nil {
				return
			}
			for _, field := range fields.List {
				for _, name := range field.Names {
					if object := info.Defs[name]; object != nil {
						flow.parameters[keyer.key(
							object,
						)] = flowParameter{goPackage: analyzedPackage, function: function, result: result}
					}
				}
			}
		}
		for _, file := range analyzedPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.FuncDecl:
					if function, isFunction := info.Defs[typed.Name].(*types.Func); isFunction {
						flow.declarations[keyer.key(
							function,
						)] = flowDeclaration{goPackage: analyzedPackage, declaration: typed}
					}
					recordParameters(typed, typed.Recv, false)
					recordParameters(typed, typed.Type.Params, false)
					recordParameters(typed, typed.Type.Results, true)
				case *ast.FuncLit:
					recordParameters(typed, typed.Type.Params, false)
					recordParameters(typed, typed.Type.Results, true)
				case *ast.Ident:
					switch object := info.Uses[typed].(type) {
					case *types.Var:
						if !object.IsField() && flow.carries(object.Type()) {
							key := keyer.key(object)
							flow.uses[key] = append(
								flow.uses[key],
								flowSite{goPackage: analyzedPackage, file: file, expression: typed},
							)
						}
					case *types.Func:
						declared := object.Origin()
						if flow.carries(declared.Signature().Results()) {
							key := keyer.key(declared)
							flow.uses[key] = append(
								flow.uses[key],
								flowSite{goPackage: analyzedPackage, file: file, expression: typed},
							)
						}
					}
				}
				return true
			})
		}
	}
}

// carries reports whether a value of a type can hold a reflect.Type, Value, StructField, Method or MapIter: one of
// those, an interface other than error, a type parameter, or anything holding one.
func (flow *typeNameFlow) carries(goType types.Type) bool {
	if goType == nil {
		return false
	}
	if answer, known := flow.carrying[goType]; known {
		return answer
	}
	flow.carrying[goType] = false
	answer := false
	switch typed := types.Unalias(goType).(type) {
	case *types.Named:
		switch {
		case typed.Obj().Pkg() != nil && typed.Obj().Pkg().Path() == "reflect":
			switch typed.Obj().Name() {
			case "Type", "Value", "StructField", "Method", "MapIter":
				answer = true
			}
		case typed.Obj().Pkg() == nil && typed.Obj().Name() == "error":
		default:
			answer = flow.carries(typed.Underlying())
		}
	case *types.Interface, *types.TypeParam:
		answer = true
	case *types.Pointer:
		answer = flow.carries(typed.Elem())
	case *types.Slice:
		answer = flow.carries(typed.Elem())
	case *types.Array:
		answer = flow.carries(typed.Elem())
	case *types.Chan:
		answer = flow.carries(typed.Elem())
	case *types.Map:
		answer = flow.carries(typed.Key()) || flow.carries(typed.Elem())
	case *types.Struct:
		for index := range typed.NumFields() {
			if flow.carries(typed.Field(index).Type()) {
				answer = true
				break
			}
		}
	case *types.Signature:
		answer = flow.carries(typed.Params()) || flow.carries(typed.Results())
	case *types.Tuple:
		for index := range typed.Len() {
			if flow.carries(typed.At(index).Type()) {
				answer = true
				break
			}
		}
	}
	flow.carrying[goType] = answer
	return answer
}

func (flow *typeNameFlow) parentsOf(file *ast.File) map[ast.Node]ast.Node {
	if parents, built := flow.parents[file]; built {
		return parents
	}
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	flow.parents[file] = parents
	return parents
}

// push queues an expression holding a reflect value, unless its type cannot hold one or it was followed in
// this mode already.
//
// Mutates: state the walk's own queue and the sites it has seen, which every step of one flow shares
func (flow *typeNameFlow) push(state *flowState, site flowSite, mode flowMode) {
	if expressionType := site.goPackage.TypesInfo.TypeOf(
		site.expression,
	); expressionType != nil && !flow.carries(expressionType) {
		return
	}
	if state.seenSites[site.expression]&mode == mode {
		return
	}
	state.seenSites[site.expression] |= mode
	state.pending = append(state.pending, flowItem{site: site, mode: mode})
}

// follow walks up from an expression holding a reflect value to what is done with it.
func (flow *typeNameFlow) follow(state *flowState, item flowItem) {
	goPackage := item.site.goPackage
	parents := flow.parentsOf(item.site.file)
	at := func(expression ast.Expr) flowSite {
		return flowSite{goPackage: goPackage, file: item.site.file, expression: expression}
	}
	var node ast.Node = item.site.expression
	for {
		switch parent := parents[node].(type) {
		case *ast.ParenExpr:
			node = parent
			continue
		case *ast.KeyValueExpr:
			node = parent
			continue
		case *ast.SelectorExpr:
			if parent.Sel == node {
				// A qualified identifier, pkg.Name: the selector is the value.
				node = parent
				continue
			}
			flow.followSelector(state, item, parent)
		case *ast.CallExpr:
			flow.followCall(state, item, parent, node)
		case *ast.AssignStmt:
			if !holds(parent.Rhs, node) {
				return
			}
			for _, target := range assignedFrom(parent.Lhs, parent.Rhs, node) {
				flow.taintTarget(state, item, target)
			}
		case *ast.ValueSpec:
			if !holds(parent.Values, node) {
				return
			}
			var targets []ast.Expr
			for _, name := range parent.Names {
				targets = append(targets, name)
			}
			for _, target := range assignedFrom(targets, parent.Values, node) {
				flow.taintTarget(state, item, target)
			}
		case *ast.RangeStmt:
			if parent.X != node {
				return
			}
			for _, bound := range []ast.Expr{parent.Key, parent.Value} {
				if bound != nil {
					flow.taintTarget(state, item, bound)
				}
			}
		case *ast.CompositeLit:
			flow.push(state, at(parent), item.mode)
		case *ast.UnaryExpr, *ast.StarExpr, *ast.TypeAssertExpr:
			// All three are expressions, so this always holds.
			if expression, isExpression := parent.(ast.Expr); isExpression {
				flow.push(state, at(expression), item.mode)
			}
		case *ast.SliceExpr:
			if parent.X == node {
				flow.push(state, at(parent), item.mode)
			}
		case *ast.IndexExpr:
			if parent.X == node {
				flow.push(state, at(parent), item.mode)
				return
			}
			// A reflect value as a map's key: stored when the index is assigned to, compared when it is read.
			if assignment, isAssignment := parents[parent].(*ast.AssignStmt); isAssignment &&
				holds(assignment.Lhs, parent) {
				flow.taintTarget(state, item, parent)
			}
		case *ast.ReturnStmt:
			flow.taintResults(state, goPackage, enclosingFunction(parents, parent), item.mode)
		case *ast.SendStmt:
			if parent.Value == node {
				state.read(item.mode)
			}
		case *ast.BinaryExpr, *ast.ExprStmt, *ast.IncDecStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.CaseClause,
			*ast.ForStmt:
			// Compared, or computed and dropped.
		default:
			state.read(item.mode)
		}
		return
	}
}

// followSelector follows a reflect value whose field is read or whose method is called.
func (flow *typeNameFlow) followSelector(state *flowState, item flowItem, selector *ast.SelectorExpr) {
	goPackage := item.site.goPackage
	selection := goPackage.TypesInfo.Selections[selector]
	if selection == nil {
		state.read(item.mode)
		return
	}
	at := func(expression ast.Expr) flowSite {
		return flowSite{goPackage: goPackage, file: item.site.file, expression: expression}
	}
	switch selection.Kind() {
	case types.FieldVal:
		flow.push(state, at(selector), item.mode)
		return
	case types.MethodExpr:
		state.read(item.mode)
		return
	}
	parents := flow.parentsOf(item.site.file)
	var called ast.Node = selector
	for {
		if paren, isParen := parents[called].(*ast.ParenExpr); isParen {
			called = paren
			continue
		}
		break
	}
	call, isCall := parents[called].(*ast.CallExpr)
	if !isCall || ast.Unparen(call.Fun) != selector {
		// A method value goes where the analysis cannot follow.
		state.read(item.mode)
		return
	}
	function, isFunction := selection.Obj().(*types.Func)
	if !isFunction {
		// A method selection's object is a *types.Func; a field call reaches what the analysis cannot follow.
		state.read(item.mode)
		return
	}
	method := function.Origin()
	if method.Pkg() != nil && method.Pkg().Path() == "reflect" {
		receiverType, _ := dereference(method.Signature().Recv().Type())
		receiverName := ""
		if named, isNamed := types.Unalias(receiverType).(*types.Named); isNamed {
			receiverName = named.Obj().Name()
		}
		if reflectTypeNameMethods[receiverName][method.Name()] {
			state.read(item.mode)
			return
		}
		mode := flowHeld
		if reflectSameTypeMethods[method.Name()] {
			mode = item.mode
		}
		flow.push(state, at(call), mode)
		return
	}
	declaration, declared := flow.declarations[flow.analysis.engine.keyer.key(method)]
	if !declared || types.IsInterface(method.Signature().Recv().Type()) || declaration.declaration.Recv == nil {
		state.read(item.mode)
		return
	}
	for _, field := range declaration.declaration.Recv.List {
		for _, name := range field.Names {
			if object, isVariable := declaration.goPackage.TypesInfo.Defs[name].(*types.Var); isVariable {
				flow.taintObject(state, object, item.mode)
			}
		}
	}
}

// followCall follows a reflect value handed to a call, or called.
func (flow *typeNameFlow) followCall(state *flowState, item flowItem, call *ast.CallExpr, node ast.Node) {
	goPackage := item.site.goPackage
	info := goPackage.TypesInfo
	at := func(expression ast.Expr) flowSite {
		return flowSite{goPackage: goPackage, file: item.site.file, expression: expression}
	}
	if call.Fun == node {
		state.read(item.mode)
		return
	}
	if value, found := info.Types[call.Fun]; found && value.IsType() {
		flow.push(state, at(call), item.mode)
		return
	}
	index := -1
	for position, argument := range call.Args {
		if argument == node {
			index = position
		}
	}
	function := ast.Unparen(call.Fun)
	switch typed := function.(type) {
	case *ast.IndexExpr:
		function = typed.X
	case *ast.IndexListExpr:
		function = typed.X
	}
	switch callee := flow.analysis.referencedObject(goPackage, function).(type) {
	case *types.Builtin:
		switch callee.Name() {
		case "append":
			flow.push(state, at(call), item.mode)
		case "copy":
			if index == 1 {
				flow.taintTarget(state, item, call.Args[0])
			}
		case "len", "cap", "delete", "clear", "min", "max":
		default:
			state.read(item.mode)
		}
	case *types.Func:
		declared := callee.Origin()
		if declared.Pkg() == nil {
			state.read(item.mode)
			return
		}
		path := declared.Pkg().Path()
		switch {
		case path == "reflect" || nameBlindPackages[path]:
			flow.push(state, at(call), item.mode)
		case flow.analysis.engine.workspacePaths[path]:
			declaration, found := flow.declarations[flow.analysis.engine.keyer.key(declared)]
			signature := declared.Signature()
			if !found || index < 0 || (signature.Recv() != nil && types.IsInterface(signature.Recv().Type())) {
				state.read(item.mode)
				return
			}
			parameterIndex := min(index, signature.Params().Len()-1)
			ordinal := 0
			for _, field := range declaration.declaration.Type.Params.List {
				for _, name := range field.Names {
					if ordinal == parameterIndex {
						if object, isVariable := declaration.goPackage.TypesInfo.Defs[name].(*types.Var); isVariable {
							flow.taintObject(state, object, item.mode)
						}
					}
					ordinal++
				}
				if len(field.Names) == 0 {
					ordinal++
				}
			}
		default:
			state.read(item.mode)
		}
	default:
		state.read(item.mode)
	}
}

// taintTarget records that an assignment's target now holds a reflect value: a variable, or the local a field,
// element or pointer target belongs to. A target reached through a parameter, or through anything but a
// variable, is someone else's, and out of sight.
func (flow *typeNameFlow) taintTarget(state *flowState, item flowItem, target ast.Expr) {
	info := item.site.goPackage.TypesInfo
	unwrapped := ast.Unparen(target)
	if identifier, isIdentifier := unwrapped.(*ast.Ident); isIdentifier {
		if identifier.Name == "_" {
			return
		}
		object := info.Defs[identifier]
		if object == nil {
			object = info.Uses[identifier]
		}
		variable, isVariable := object.(*types.Var)
		if !isVariable {
			state.read(item.mode)
			return
		}
		if flow.carries(variable.Type()) {
			flow.taintObject(state, variable, item.mode)
		}
		return
	}
	root := rootVariable(info, unwrapped)
	if root == nil {
		state.read(item.mode)
		return
	}
	if parameter, isParameter := flow.parameters[flow.analysis.engine.keyer.key(
		root,
	)]; isParameter && !parameter.result {
		state.read(item.mode)
		return
	}
	flow.taintObject(state, root, item.mode)
}

// rootVariable is the variable a field, element or pointer target belongs to, or nil.
func rootVariable(info *types.Info, target ast.Expr) *types.Var {
	current := target
	for {
		switch typed := ast.Unparen(current).(type) {
		case *ast.Ident:
			variable, _ := info.Uses[typed].(*types.Var)
			return variable
		case *ast.SelectorExpr:
			if selection := info.Selections[typed]; selection != nil {
				current = typed.X
				continue
			}
			variable, _ := info.Uses[typed.Sel].(*types.Var)
			return variable
		case *ast.IndexExpr:
			current = typed.X
		case *ast.StarExpr:
			current = typed.X
		default:
			return nil
		}
	}
}

// taintObject follows every use of a variable now holding a reflect value. A package-level variable another
// module may read is out of sight; a named result is returned.
func (flow *typeNameFlow) taintObject(state *flowState, variable *types.Var, mode flowMode) {
	if variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() && escapesWorkspace(variable) {
		state.read(mode)
		return
	}
	key := flow.analysis.engine.keyer.key(variable)
	if state.seenObjects[key]&mode == mode {
		return
	}
	state.seenObjects[key] |= mode
	for _, use := range flow.uses[key] {
		flow.push(state, use, mode)
	}
	if parameter, isParameter := flow.parameters[key]; isParameter && parameter.result {
		flow.taintResults(state, parameter.goPackage, parameter.function, mode)
	}
}

// taintResults follows a function's results, now holding a reflect value, to every call of it. A method's
// callers include those through an interface, and a function literal's those of whatever holds it, so those
// leave the analysis, as does an exported function another module may call.
func (flow *typeNameFlow) taintResults(
	state *flowState,
	goPackage *packages.Package,
	function ast.Node,
	mode flowMode,
) {
	declaration, isDeclaration := function.(*ast.FuncDecl)
	if !isDeclaration || declaration.Recv != nil {
		state.read(mode)
		return
	}
	declared, isFunction := goPackage.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !isFunction || escapesWorkspace(declared) {
		state.read(mode)
		return
	}
	key := flow.analysis.engine.keyer.key(declared)
	if state.seenResults[key]&mode == mode {
		return
	}
	state.seenResults[key] |= mode
	for _, use := range flow.uses[key] {
		parents := flow.parentsOf(use.file)
		var node ast.Node = use.expression
		for {
			switch parent := parents[node].(type) {
			case *ast.SelectorExpr:
				if parent.Sel == node {
					node = parent
					continue
				}
			case *ast.IndexExpr:
				if parent.X == node {
					node = parent
					continue
				}
			case *ast.IndexListExpr:
				if parent.X == node {
					node = parent
					continue
				}
			case *ast.ParenExpr:
				node = parent
				continue
			}
			break
		}
		call, isCall := parents[node].(*ast.CallExpr)
		if !isCall || call.Fun != node {
			// The function as a value: whatever calls it is out of sight.
			state.read(mode)
			continue
		}
		flow.push(state, flowSite{goPackage: use.goPackage, file: use.file, expression: call}, mode)
	}
}

// escapesWorkspace reports whether code outside the workspace can name an object: an exported name of a library
// package outside internal/.
func escapesWorkspace(object types.Object) bool {
	if !object.Exported() || object.Pkg() == nil {
		return false
	}
	name := object.Pkg().Name()
	return !isInternal(object.Pkg().Path()) && name != "main" && !strings.HasSuffix(name, "_test")
}

// enclosingFunction is the function declaration or literal a node is in.
func enclosingFunction(parents map[ast.Node]ast.Node, node ast.Node) ast.Node {
	for current := parents[node]; current != nil; current = parents[current] {
		switch current.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return current
		}
	}
	return nil
}

// holds reports whether a list holds an expression itself.
func holds(list []ast.Expr, node ast.Node) bool {
	for _, expression := range list {
		if expression == node {
			return true
		}
	}
	return false
}

// assignedFrom is the targets one right-hand expression assigns to: its partner, or every target of a tuple.
func assignedFrom(targets []ast.Expr, values []ast.Expr, node ast.Node) []ast.Expr {
	if len(targets) != len(values) {
		return targets
	}
	for index, value := range values {
		if value == node {
			return []ast.Expr{targets[index]}
		}
	}
	return nil
}

// formatTypeNameCall is what a call of a printf-like function outside the workspace reads of each argument's type
// name, by the argument's index: %T reads its own type's name, %#v that and every name it holds. A printf-like
// function takes a string parameter named format, then `...any`, as fmt.Errorf, log.Printf and testing's Fatalf do.
// A format that is not a constant may do either to every operand.
func formatTypeNameCall(info *types.Info, call *ast.CallExpr, signature *types.Signature) map[int]encoder {
	parameters := signature.Params()
	count := parameters.Len()
	if !signature.Variadic() || count < 2 || parameters.At(count-2).Name() != "format" {
		return nil
	}
	if basic, isBasic := parameters.At(
		count - 2,
	).Type().Underlying().(*types.Basic); !isBasic || basic.Kind() != types.String {
		return nil
	}
	if slice, isSlice := parameters.At(count - 1).Type().(*types.Slice); !isSlice || !isEmptyInterface(slice.Elem()) {
		return nil
	}
	if len(call.Args) < count-1 {
		return nil
	}
	both := encoderFormatType | encoderFormatGoSyntax
	reads := map[int]encoder{}
	value := info.Types[call.Args[count-2]].Value
	if value == nil || value.Kind() != constant.String {
		for index := count - 1; index < len(call.Args); index++ {
			reads[index] = both
		}
		return reads
	}
	operands := formatTypeNameOperands(constant.StringVal(value))
	if call.Ellipsis.IsValid() {
		all := encoder(0)
		for _, set := range operands {
			all |= set
		}
		if all != 0 {
			reads[count-1] = all
		}
		return reads
	}
	for operand, set := range operands {
		reads[count-1+operand] = set
	}
	return reads
}

// formatTypeNameOperands reads a format string as fmt does and maps each operand, by its index, to what the verb
// it is printed with reads of its type's name: %T reads encoderFormatType, %#v encoderFormatGoSyntax. Widths and
// precisions given by `*` take an operand, and an explicit `[n]` moves to the n-th.
func formatTypeNameOperands(format string) map[int]encoder {
	operands := map[int]encoder{}
	operand := 0
	position := 0
	readIndex := func() {
		if position >= len(format) || format[position] != '[' {
			return
		}
		end := strings.IndexByte(format[position:], ']')
		if end < 0 {
			return
		}
		if number, numberError := strconv.Atoi(format[position+1 : position+end]); numberError == nil && number >= 1 {
			operand = number - 1
		}
		position += end + 1
	}
	readNumber := func() {
		if position < len(format) && format[position] == '*' {
			operand++
			position++
			return
		}
		for position < len(format) && format[position] >= '0' && format[position] <= '9' {
			position++
		}
	}
	for position < len(format) {
		if format[position] != '%' {
			position++
			continue
		}
		position++
		sharp := false
		for position < len(format) && strings.IndexByte("+-# 0", format[position]) >= 0 {
			if format[position] == '#' {
				sharp = true
			}
			position++
		}
		readIndex()
		readNumber()
		if position < len(format) && format[position] == '.' {
			position++
			readIndex()
			readNumber()
		}
		readIndex()
		if position >= len(format) {
			break
		}
		verb, size := utf8.DecodeRuneInString(format[position:])
		position += size
		if verb == '%' {
			continue
		}
		switch {
		case verb == 'T':
			operands[operand] |= encoderFormatType
		case verb == 'v' && sharp:
			operands[operand] |= encoderFormatGoSyntax
		}
		operand++
	}
	return operands
}
