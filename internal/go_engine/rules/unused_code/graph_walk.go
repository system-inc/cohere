package unused_code

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/go/ast/astutil"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
)

// read records that by uses every object syntax reads. Among many other things, an identifier's use handles (7.1)
// field accesses use fields.
func (graph *graph) read(syntax ast.Node, by types.Object) {
	if syntax == nil {
		return
	}

	switch syntax := syntax.(type) {
	case *ast.Ident:
		// Among many other things, this handles
		// (7.1) field accesses use fields
		graph.use(graph.typesInfo.ObjectOf(syntax), by)

	case *ast.BasicLit:
		// Nothing to do

	case *ast.SliceExpr:
		graph.read(syntax.X, by)
		graph.read(syntax.Low, by)
		graph.read(syntax.High, by)
		graph.read(syntax.Max, by)

	case *ast.UnaryExpr:
		graph.read(syntax.X, by)

	case *ast.ParenExpr:
		graph.read(syntax.X, by)

	case *ast.ArrayType:
		graph.read(syntax.Len, by)
		graph.read(syntax.Elt, by)

	case *ast.SelectorExpr:
		graph.readSelectorExpression(syntax, by)

	case *ast.IndexExpr:
		// Among many other things, this handles
		// (2.6) named types use all their type arguments
		graph.read(syntax.X, by)
		graph.read(syntax.Index, by)

	case *ast.IndexListExpr:
		// Among many other things, this handles
		// (2.6) named types use all their type arguments
		graph.read(syntax.X, by)
		for _, index := range syntax.Indices {
			graph.read(index, by)
		}

	case *ast.BinaryExpr:
		graph.read(syntax.X, by)
		graph.read(syntax.Y, by)

	case *ast.CompositeLit:
		graph.readCompositeLiteral(syntax, by)

	case *ast.KeyValueExpr:
		graph.read(syntax.Key, by)
		graph.read(syntax.Value, by)

	case *ast.StarExpr:
		graph.read(syntax.X, by)

	case *ast.MapType:
		graph.read(syntax.Key, by)
		graph.read(syntax.Value, by)

	case *ast.FuncLit:
		graph.read(syntax.Type, by)

		// See graph.declaration's handling of ast.FuncDecl for why this bit of code is necessary.
		if signature, isSignature := graph.typesInfo.TypeOf(syntax).(*types.Signature); isSignature {
			for parameters, index := signature.Params(), 0; index < parameters.Len(); index++ {
				graph.see(parameters.At(index), by)
				if parameters.At(index).Name() == "" {
					graph.use(parameters.At(index), by)
				}
			}
		}

		graph.block(syntax.Body, by)

	case *ast.FuncType:
		// DefaultOptions' ParametersAreUsed: seeScope uses every parameter with the other local variables, where the
		// original passes it the parameters to skip under the other setting.
		graph.seeScope(syntax, by)

		// (4.1) functions use all their arguments, return parameters and receivers
		// (12.1) type parameters use their constraint type
		graph.read(syntax.TypeParams, by)
		graph.read(syntax.Params, by)
		graph.read(syntax.Results, by)

	case *ast.FieldList:
		if syntax == nil {
			return
		}

		// This branch is only hit for field lists enclosed by parentheses or square brackets, i.e. parameters. Fields
		// (for structs) and method lists (for interfaces) are handled elsewhere.

		for _, field := range syntax.List {
			if len(field.Names) == 0 {
				graph.read(field.Type, by)
			} else {
				for _, name := range field.Names {
					// The original considers emitting by -> type instead of by -> name -> type, since the usedness
					// of parameters of any kind does not matter.
					parameter := graph.typesInfo.ObjectOf(name)
					graph.use(parameter, by)
					graph.read(field.Type, parameter)
				}
			}
		}

	case *ast.ChanType:
		graph.read(syntax.Value, by)

	case *ast.StructType:
		// This is only used for anonymous struct types, not named ones.

		for _, field := range syntax.Fields.List {
			if len(field.Names) == 0 {
				// embedded field

				if embedded := graph.embeddedField(field.Type, by); embedded != nil {
					graph.use(embedded, by)
				}
			} else {
				for _, name := range field.Names {
					// (11.1) anonymous struct types use all their fields
					declared := graph.typesInfo.ObjectOf(name)
					graph.see(declared, by)
					graph.use(declared, by)
					graph.read(field.Type, declared)
				}
			}
		}

	case *ast.TypeAssertExpr:
		graph.read(syntax.X, by)
		graph.read(syntax.Type, by)

	case *ast.InterfaceType:
		graph.readInterfaceType(syntax, by)

	case *ast.Ellipsis:
		graph.read(syntax.Elt, by)

	case *ast.CallExpr:
		graph.readCall(syntax, by)

	default:
		lint.ExhaustiveTypeSwitch(syntax)
	}
}

// readCompositeLiteral reads a composite literal. The original reads it inside read, and the port gives it a function
// of its own, as it does a call and an interface type, to keep read one switch a reader can hold.
func (graph *graph) readCompositeLiteral(literal *ast.CompositeLit, by types.Object) {
	graph.read(literal.Type, by)
	// We get the type of the node itself, not of node.Type, to handle nested composite literals of the kind T{{...}}
	structType, isStruct := core_type.Of(graph.typesInfo.TypeOf(literal)).(*types.Struct)
	if !isStruct {
		for _, element := range literal.Elts {
			graph.read(element, by)
		}
		return
	}

	// DefaultOptions' FieldWritesAreUses: every field a literal writes is used, so the literal's elements are read
	// whether or not they are keyed, and the original's branch writing each key under the other setting is gone.
	unkeyed := false
	if len(literal.Elts) != 0 {
		_, firstIsKeyed := literal.Elts[0].(*ast.KeyValueExpr)
		unkeyed = !firstIsKeyed
	}
	if unkeyed {
		// Untagged struct literal that specifies all fields. We have to manually use the fields in the type, because
		// the unkeyed literal doesn't contain any nodes referring to the fields.
		for field := range structType.Fields() {
			graph.use(field, by)
		}
	}
	for _, element := range literal.Elts {
		graph.read(element, by)
	}
	if unkeyed {
		return
	}
	for _, element := range literal.Elts {
		keyValue, isKeyValue := element.(*ast.KeyValueExpr)
		if !isKeyValue {
			continue
		}
		key, isIdentifier := keyValue.Key.(*ast.Ident)
		if !isIdentifier {
			continue
		}
		_, path, _ := types.LookupFieldOrMethod(structType, true, graph.typesPackage, key.Name)
		if len(path) == 0 {
			continue
		}

		current := structType
		for _, step := range path[:len(path)-1] {
			field := current.Field(step)
			graph.use(field, by)
			next, isNextStruct := core_type.Of(field.Type()).(*types.Struct)
			if !isNextStruct {
				break
			}
			current = next
		}
	}
}

// readInterfaceType reads an interface type, recording it among the interfaces a named type may implement.
func (graph *graph) readInterfaceType(interfaceSyntax *ast.InterfaceType, by types.Object) {
	if len(interfaceSyntax.Methods.List) != 0 {
		if interfaceType, isInterface := graph.typesInfo.TypeOf(interfaceSyntax).(*types.Interface); isInterface {
			graph.interfaceTypes = append(graph.interfaceTypes, interfaceType)
		}
	}
	for _, method := range interfaceSyntax.Methods.List {
		switch len(method.Names) {
		case 0:
			// Embedded type or type union
			// (8.4) all embedded interfaces are marked as used
			// (this also covers type sets)

			graph.read(method.Type, by)
		case 1:
			// Method
			// (8.3) all interface methods are marked as used
			declared := graph.typesInfo.ObjectOf(method.Names[0])
			graph.see(declared, by)
			graph.use(declared, by)
			graph.read(method.Type, declared)
		default:
			// Invariant: an interface's method list names one method per entry, or none for an embedded type.
			panic(fmt.Sprintf("unexpected number of names: %d", len(method.Names)))
		}
	}
}

// readCall reads a call, and a conversion's struct fields.
func (graph *graph) readCall(call *ast.CallExpr, by types.Object) {
	graph.read(call.Fun, by)
	for _, argument := range call.Args {
		graph.read(argument, by)
	}

	// Handle conversions
	if len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return
	}

	destination := graph.typesInfo.TypeOf(call.Fun)
	source := graph.typesInfo.TypeOf(call.Args[0])

	// The original notes that this should use DereferenceR instead, guarded against infinite recursion.
	sourceCore := core_type.Of(typeutil.Dereference(source))
	destinationCore := core_type.Of(typeutil.Dereference(destination))
	sourceStruct, isSourceStruct := sourceCore.(*types.Struct)
	destinationStruct, isDestinationStruct := destinationCore.(*types.Struct)
	if isDestinationStruct && isSourceStruct {
		// Converting between two structs. The fields are relevant for the conversion, but only if the fields are also
		// used outside of the conversion. Mark fields as used by each other.

		if destinationStruct.NumFields() != sourceStruct.NumFields() {
			// Invariant: two struct types convert only when their fields are identical but for tags.
			panic("failed assertion")
		}
		for index := 0; index < destinationStruct.NumFields(); index++ {
			// (5.1) when converting between two equivalent structs, the fields in either struct use each other. the
			// fields are relevant for the conversion, but only if the fields are also accessed outside the
			// conversion.
			graph.use(destinationStruct.Field(index), sourceStruct.Field(index))
			graph.use(sourceStruct.Field(index), destinationStruct.Field(index))
		}
	} else if isSourceStruct && destinationCore == types.Typ[types.UnsafePointer] {
		// (5.2) when converting to or from unsafe.Pointer, mark all fields as used.
		graph.useAllFieldsRecursively(sourceStruct, by)
	} else if isDestinationStruct && sourceCore == types.Typ[types.UnsafePointer] {
		// (5.2) when converting to or from unsafe.Pointer, mark all fields as used.
		graph.useAllFieldsRecursively(destinationStruct, by)
	}
}

// useAllFieldsRecursively uses every field of goType, and every field of theirs, through arrays too.
func (graph *graph) useAllFieldsRecursively(goType types.Type, by types.Object) {
	switch underlying := goType.Underlying().(type) {
	case *types.Struct:
		for field := range underlying.Fields() {
			graph.use(field, by)
			graph.useAllFieldsRecursively(field.Type(), by)
		}
	case *types.Array:
		graph.useAllFieldsRecursively(underlying.Elem(), by)
	default:
		return
	}
}

// write records what by uses by writing to syntax.
func (graph *graph) write(syntax ast.Node, by types.Object) {
	if syntax == nil {
		return
	}

	switch syntax := syntax.(type) {
	case *ast.Ident:
		written := graph.typesInfo.ObjectOf(syntax)
		if written == nil {
			// This can happen for `switch x := v.(type)`, where that x doesn't have an object
			return
		}

		// (4.9) functions use package-level variables they assign to iff in tests (sinks for benchmarks)
		// (9.7) variable _reads_ use variables, writes do not, except in tests
		// The written variable may be another package's, `other.Value = x`, so this reads the name of a file of
		// another package, which no build read records (#b2t9xmf). It decides only whether that variable is used, and
		// the rule reports the judged package's own objects alone, so another build's answer changes no finding here.
		path := graph.fileSet.File(written.Pos()).Name()
		if strings.HasSuffix(path, "_test.go") {
			if isGlobal(written) {
				graph.use(written, by)
			}
		}

	case *ast.IndexExpr:
		graph.read(syntax.X, by)
		graph.read(syntax.Index, by)

	case *ast.SelectorExpr:
		// DefaultOptions' FieldWritesAreUses: writing to a field constitutes a use. See
		// https://staticcheck.dev/issues/288 for some discussion on that.
		//
		// This code can also get triggered by qualified package variables, in which case it doesn't matter what we
		// do, because the object is in another package. The original notes this would not hold were the usedness of
		// exported identifiers tracked.
		graph.readSelectorExpression(syntax, by)

	case *ast.StarExpr:
		graph.read(syntax.X, by)

	case *ast.ParenExpr:
		graph.write(syntax.X, by)

	default:
		lint.ExhaustiveTypeSwitch(syntax)
	}
}

// readSelectorExpression reads all elements of a selector expression, including implicit fields.
func (graph *graph) readSelectorExpression(selector *ast.SelectorExpr, by types.Object) {
	// cover syntax-based accesses
	graph.read(selector.X, by)
	graph.read(selector.Sel, by)

	selection, found := graph.typesInfo.Selections[selector]
	if !found {
		return
	}
	graph.readSelection(selection, by)
}

// readSelection uses the object a selection selects, and every embedded field its path passes through.
func (graph *graph) readSelection(selection *types.Selection, by types.Object) {
	path := selection.Index()
	base := selection.Recv()
	for _, step := range path[:len(path)-1] {
		// The original asks whether core types are needed here.
		structType, isStruct := typeutil.Dereference(base.Underlying()).Underlying().(*types.Struct)
		if !isStruct {
			return
		}
		field := structType.Field(step)
		graph.use(field, by)
		base = field.Type()
	}

	graph.use(selection.Obj(), by)
}

// block sees the objects a block's scope declares and walks its statements.
func (graph *graph) block(block *ast.BlockStmt, by types.Object) {
	if block == nil {
		return
	}

	graph.seeScope(block, by)
	for _, statement := range block.List {
		graph.statement(statement, by)
	}
}

// isGlobal is whether typesObject is declared at its package's scope.
func isGlobal(typesObject types.Object) bool {
	return typesObject.Parent() == typesObject.Pkg().Scope()
}

// declaration walks one declaration, the original's decl.
func (graph *graph) declaration(declaration ast.Decl, by types.Object) {
	switch declaration := declaration.(type) {
	case *ast.GenDecl:
		switch declaration.Tok {
		case token.IMPORT:
			// Nothing to do

		case token.CONST:
			graph.constantDeclaration(declaration, by)

		case token.TYPE:
			for _, spec := range declaration.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if !isTypeSpec {
					continue
				}
				typeName, isTypeName := graph.typesInfo.ObjectOf(typeSpec.Name).(*types.TypeName)
				if !isTypeName {
					continue
				}
				graph.see(typeName, by)
				graph.seeScope(typeSpec, typeName)
				if !typeSpec.Assign.IsValid() {
					graph.namedTypes = append(graph.namedTypes, typeName)
				}
				// DefaultOptions' ExportedIsUsed.
				if token.IsExported(typeSpec.Name.Name) && isGlobal(typeName) {
					// (1.1) packages use exported named types
					graph.use(typeName, nil)
				}

				// (2.5) named types use all their type parameters
				graph.read(typeSpec.TypeParams, typeName)

				graph.namedType(typeName, typeSpec.Type)

				if typeSpec.Name.Name == "_" {
					// (9.9) objects named the blank identifier are used
					graph.use(typeName, by)
				}
			}

		case token.VAR:
			graph.variableDeclaration(declaration, by)

		default:
			// Invariant: a general declaration is of imports, constants, types or variables.
			panic(fmt.Sprintf("unexpected token %s", declaration.Tok))
		}

	case *ast.FuncDecl:
		graph.functionDeclaration(declaration)

	default:
		// We do not cover BadDecl, but we shouldn't ever see one of those
		lint.ExhaustiveTypeSwitch(declaration)
	}
}

// constantDeclaration walks a const declaration, and rings each group of its constants.
func (graph *graph) constantDeclaration(declaration *ast.GenDecl, by types.Object) {
	for _, spec := range declaration.Specs {
		valueSpec, isValueSpec := spec.(*ast.ValueSpec)
		if !isValueSpec {
			continue
		}
		if len(valueSpec.Values) != 0 && len(valueSpec.Values) != len(valueSpec.Names) {
			// Invariant: a constant spec gives each name its own value, or repeats the previous spec's.
			panic("failed assertion")
		}
		for index, name := range valueSpec.Names {
			constant := graph.typesInfo.ObjectOf(name)
			graph.see(constant, by)
			graph.read(valueSpec.Type, constant)

			if len(valueSpec.Values) != 0 {
				graph.read(valueSpec.Values[index], constant)
			}

			if name.Name == "_" {
				// (9.9) objects named the blank identifier are used
				graph.use(constant, by)
			} else if token.IsExported(name.Name) && isGlobal(constant) {
				// (1.4) packages use exported constants: DefaultOptions' ExportedIsUsed.
				graph.use(constant, nil)
			}
		}
	}

	for _, group := range astutil.GroupSpecs(graph.fileSet, declaration.Specs) {
		// (10.1) if one constant out of a block of constants is used, mark all of them used
		//
		// We encode this as a ring. If we have a constant group 'const ( a; b; c )', then we'll produce the following
		// graph: a -> b -> c -> a.

		var first types.Object
		var previous types.Object
		var last types.Object
		for _, spec := range group {
			valueSpec, isValueSpec := spec.(*ast.ValueSpec)
			if !isValueSpec {
				continue
			}
			for _, name := range valueSpec.Names {
				if name.Name == "_" {
					// Having a blank constant in a group doesn't mark the whole group as used
					continue
				}

				constant := graph.typesInfo.ObjectOf(name)
				if first == nil {
					first = constant
				} else {
					graph.use(constant, previous)
				}
				previous = constant
				last = constant
			}
		}
		if first != nil && first != last {
			graph.use(first, last)
		}
	}
}

// variableDeclaration walks a var declaration.
func (graph *graph) variableDeclaration(declaration *ast.GenDecl, by types.Object) {
	// We cannot rely on types.Initializer for package-level variables because
	// - initializers are only tracked for variables that are actually initialized
	// - we want to see the syntax of the type, if specified, not just the right-hand side

	for _, spec := range declaration.Specs {
		valueSpec, isValueSpec := spec.(*ast.ValueSpec)
		if !isValueSpec {
			continue
		}
		for index, name := range valueSpec.Names {
			variable := graph.typesInfo.ObjectOf(name)
			graph.see(variable, by)
			// variables and constants use their types
			graph.read(valueSpec.Type, variable)

			if len(valueSpec.Names) == len(valueSpec.Values) {
				// One value per variable
				graph.read(valueSpec.Values[index], variable)
			} else if len(valueSpec.Values) != 0 {
				// Multiple variables initialized with a single right-hand side
				if len(valueSpec.Values) != 1 {
					// Invariant: variables a spec does not give a value each share the one value of a call.
					panic(graph.fileSet.PositionFor(valueSpec.Pos(), false))
				}
				graph.read(valueSpec.Values[0], variable)
			}

			// DefaultOptions' ExportedIsUsed.
			if token.IsExported(name.Name) && isGlobal(variable) {
				// (1.3) packages use exported variables
				graph.use(variable, nil)
			}

			if name.Name == "_" {
				// (9.9) objects named the blank identifier are used
				graph.use(variable, by)
			}
		}
	}
}

// functionDeclaration walks a function's declaration, and uses it when it is an entry point.
func (graph *graph) functionDeclaration(declaration *ast.FuncDecl) {
	function, isFunction := graph.typesInfo.ObjectOf(declaration.Name).(*types.Func)
	if !isFunction {
		return
	}
	declared := function.Origin()
	graph.see(declared, nil)

	// DefaultOptions' ExportedIsUsed.
	if token.IsExported(declaration.Name.Name) {
		if declaration.Recv == nil {
			// (1.2) packages use exported functions
			graph.use(declared, nil)
		}
	} else if declaration.Name.Name == "init" {
		// (1.5) packages use init functions
		graph.use(declared, nil)
	} else if declaration.Name.Name == "main" && graph.typesPackage.Name() == "main" {
		// (1.7) packages use the main function iff in the main package
		graph.use(declared, nil)
	} else if graph.typesPackage.Path() == "runtime" && runtimeFunctions[declaration.Name.Name] {
		// (9.8) runtime functions that may be called from user code via the compiler
		graph.use(declared, nil)
	} else if graph.typesPackage.Path() == "runtime/coverage" && runtimeCoverageFunctions[declaration.Name.Name] {
		// (9.8) runtime functions that may be called from user code via the compiler
		graph.use(declared, nil)
	}

	// (4.1) functions use their receivers
	graph.read(declaration.Recv, declared)
	graph.read(declaration.Type, declared)
	graph.block(declaration.Body, declared)

	// graph.read(declaration.Type) will ultimately call graph.seeScope and see parameters that way. But because it
	// relies entirely on the syntax, it cannot resolve unnamed parameters to types.Object. For that reason we
	// explicitly handle arguments here, as well as for function literals elsewhere.
	//
	// graph.seeScope can't get to the types.Signature for this function because there is no mapping from
	// ast.FuncType to types.Signature, only from ast.Ident to types.Signature.
	//
	// The original notes this is only really relevant when ParametersAreUsed is false. Otherwise, all parameters are
	// considered used, and if we never see a parameter then no harm done (we still see its type separately).
	if signature, isSignature := graph.typesInfo.TypeOf(declaration.Name).(*types.Signature); isSignature {
		for parameters, index := signature.Params(), 0; index < parameters.Len(); index++ {
			graph.see(parameters.At(index), declared)
			if parameters.At(index).Name() == "" {
				graph.use(parameters.At(index), declared)
			}
		}
	}

	if declaration.Name.Name == "_" {
		// (9.9) objects named the blank identifier are used
		graph.use(declared, nil)
	}

	if declaration.Doc != nil {
		for _, comment := range declaration.Doc.List {
			if strings.HasPrefix(comment.Text, "//go:cgo_export_") {
				// (1.6) packages use functions exported to cgo
				graph.use(declared, nil)
			}
		}
	}
}

// seeScope sees all objects in syntax's scope, and uses every one that isn't a field: DefaultOptions'
// LocalVariablesAreUsed. The original also takes variables to skip, which only ParametersAreUsed false fills, so the
// port leaves that parameter out.
func (graph *graph) seeScope(syntax ast.Node, by types.Object) {
	// A note on functions and scopes: for a function declaration, the body's BlockStmt can't be found in
	// types.Info.Scopes. Instead, the FuncType can, and that scope will contain receivers, parameters, return
	// parameters and immediate local variables.

	scope := graph.typesInfo.Scopes[syntax]
	if scope == nil {
		return
	}
	for _, name := range scope.Names() {
		declared := scope.Lookup(name)
		graph.see(declared, by)

		if variable, isVariable := declared.(*types.Var); isVariable && !variable.IsField() {
			graph.use(variable, by)
		}
	}
}

// withoutLabels is statement with every label around it unwrapped. We don't care about labels, and a label can
// itself be labeled.
func withoutLabels(statement ast.Stmt) ast.Stmt {
	unlabeled := statement
	for {
		labeled, isLabeled := unlabeled.(*ast.LabeledStmt)
		if !isLabeled {
			return unlabeled
		}
		unlabeled = labeled.Stmt
	}
}

// statement walks one statement, the original's stmt.
func (graph *graph) statement(statement ast.Stmt, by types.Object) {
	if statement == nil {
		return
	}

	switch statement := withoutLabels(statement).(type) {
	case *ast.AssignStmt:
		for _, left := range statement.Lhs {
			graph.write(left, by)
		}
		for _, right := range statement.Rhs {
			// Note: it would be more accurate to have the right-hand side used by the left-hand side, but it
			// ultimately doesn't matter, because local variables always end up used, anyway. The original notes this
			// changes once the usedness of parameters is tracked.
			graph.read(right, by)
		}

	case *ast.BlockStmt:
		graph.block(statement, by)

	case *ast.BranchStmt:
		// Nothing to do

	case *ast.DeclStmt:
		graph.declaration(statement.Decl, by)

	case *ast.DeferStmt:
		graph.read(statement.Call, by)

	case *ast.ExprStmt:
		graph.read(statement.X, by)

	case *ast.ForStmt:
		graph.seeScope(statement, by)
		graph.statement(statement.Init, by)
		graph.read(statement.Cond, by)
		graph.statement(statement.Post, by)
		graph.block(statement.Body, by)

	case *ast.GoStmt:
		graph.read(statement.Call, by)

	case *ast.IfStmt:
		graph.seeScope(statement, by)
		graph.statement(statement.Init, by)
		graph.read(statement.Cond, by)
		graph.block(statement.Body, by)
		graph.statement(statement.Else, by)

	case *ast.IncDecStmt:
		// DefaultOptions' PostStatementsAreReads is false: we treat post-increment as a write only. This ends up
		// using fields, and sinks in tests, but not other variables.
		graph.write(statement.X, by)

	case *ast.RangeStmt:
		graph.seeScope(statement, by)

		graph.write(statement.Key, by)
		graph.write(statement.Value, by)
		graph.read(statement.X, by)
		graph.block(statement.Body, by)

	case *ast.ReturnStmt:
		for _, returned := range statement.Results {
			graph.read(returned, by)
		}

	case *ast.SelectStmt:
		graph.selectStatement(statement, by)

	case *ast.SendStmt:
		graph.read(statement.Chan, by)
		graph.read(statement.Value, by)

	case *ast.SwitchStmt:
		graph.seeScope(statement, by)
		graph.statement(statement.Init, by)
		graph.read(statement.Tag, by)
		graph.caseClauses(statement.Body, by)

	case *ast.TypeSwitchStmt:
		graph.seeScope(statement, by)
		graph.statement(statement.Init, by)
		graph.statement(statement.Assign, by)
		graph.caseClauses(statement.Body, by)

	case *ast.EmptyStmt:
		// Nothing to do

	default:
		lint.ExhaustiveTypeSwitch(statement)
	}
}

// selectStatement walks a select statement's clauses.
func (graph *graph) selectStatement(statement *ast.SelectStmt, by types.Object) {
	for _, each := range statement.Body.List {
		clause, isClause := each.(*ast.CommClause)
		if !isClause {
			continue
		}
		graph.seeScope(clause, by)
		switch communication := clause.Comm.(type) {
		case *ast.SendStmt:
			graph.read(communication.Chan, by)
			graph.read(communication.Value, by)
		case *ast.ExprStmt:
			if receive, isReceive := ast.Unparen(communication.X).(*ast.UnaryExpr); isReceive {
				graph.read(receive.X, by)
			}
		case *ast.AssignStmt:
			for _, left := range communication.Lhs {
				graph.write(left, by)
			}
			for _, right := range communication.Rhs {
				graph.read(right, by)
			}
		case nil:
		default:
			lint.ExhaustiveTypeSwitch(communication)
		}
		for _, inner := range clause.Body {
			graph.statement(inner, by)
		}
	}
}

// caseClauses walks the clauses of a switch or a type switch. The original walks each switch's clauses in its own
// case, the same code twice.
func (graph *graph) caseClauses(body *ast.BlockStmt, by types.Object) {
	for _, each := range body.List {
		clause, isClause := each.(*ast.CaseClause)
		if !isClause {
			continue
		}
		graph.seeScope(clause, by)
		for _, expression := range clause.List {
			graph.read(expression, by)
		}
		for _, inner := range clause.Body {
			graph.statement(inner, by)
		}
	}
}
