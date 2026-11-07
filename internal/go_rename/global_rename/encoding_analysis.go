package global_rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	encoderJson encoder = 1 << iota
	encoderXml
	encoderGob
	encoderYaml
	encoderToml
	encoderReflect
	encoderSlog
	encoderTemplate
	// The readers of a type's name, which no field tag changes: see type_name_flow.go. encoderHeldTypeName is a reflect
	// value of a type a value holds, reached through Field, Key or Index, which reads the names of what the type
	// holds and not its own; markType passes it down a level as encoderReflectTypeName.
	encoderReflectTypeName
	encoderHeldTypeName
	encoderFormatType
	encoderFormatGoSyntax
	encoderGobRegister
)

// typeNameFamilies are the families that read a type's name, never a field's.
const typeNameFamilies = encoderReflectTypeName | encoderHeldTypeName | encoderFormatType | encoderFormatGoSyntax |
	encoderGobRegister

// typeNameReaders are the families that read the name of a type a value reaches them with: the type-name
// families, xml, which names an element for its type, and a template, which can print %T.
const typeNameReaders = encoderReflectTypeName | encoderFormatType | encoderFormatGoSyntax | encoderGobRegister |
	encoderXml | encoderTemplate

// encoderNames lists the families in report order, with the struct tag that names a key for each, empty when
// no tag can (gob reads the Go name always; a template selects it by name).
var encoderNames = []struct {
	encoder encoder
	name    string
	tag     string
}{
	{encoderJson, "json", "json"},
	{encoderXml, "xml", "xml"},
	{encoderGob, "gob", ""},
	{encoderYaml, "yaml", "yaml"},
	{encoderToml, "toml", "toml"},
	{encoderReflect, "reflect", "json"},
	{encoderSlog, "slog", "json"},
	{encoderTemplate, "template", ""},
	{encoderReflectTypeName, "reflect.Type's Name or String", ""},
	{encoderHeldTypeName, "reflect.Type's Name or String of a type it holds", ""},
	{encoderFormatType, "fmt's %T", ""},
	{encoderFormatGoSyntax, "fmt's %#v", ""},
	{encoderGobRegister, "gob.Register", ""},
}

// encoderOf is the family a package's functions belong to, or zero. A function of one of these packages
// reads every value handed to it as an empty interface: json.Marshal, (*json.Decoder).Decode, gob.Register,
// reflect.ValueOf, slog.Any, (*template.Template).Execute. reflect.DeepEqual is the one exception, since it
// never reads a name.
func encoderOf(function *types.Func) encoder {
	if function.Pkg() == nil {
		return 0
	}
	path := function.Pkg().Path()
	switch {
	case path == "encoding/json":
		return encoderJson
	case path == "encoding/xml":
		return encoderXml
	case path == "encoding/gob":
		return encoderGob
	case strings.Contains(path, "yaml"):
		return encoderYaml
	case strings.Contains(path, "toml"):
		return encoderToml
	case path == "reflect":
		if function.Name() == "DeepEqual" {
			return 0
		}
		return encoderReflect
	case path == "log/slog":
		return encoderSlog
	case path == "text/template" || path == "html/template":
		return encoderTemplate
	}
	return 0
}

// valueSite is one expression a variable or field may hold, in the package that spells it, and which of its
// results when it yields several (`value, ok := decode()`).
type valueSite struct {
	goPackage  *packages.Package
	expression ast.Expr
	result     int
	where      token.Pos
}

// siteKey is one expression reached for one of its results.
type siteKey struct {
	expression ast.Expr
	result     int
}

// encodingAnalysis finds every struct field and named type a value handed to an encoder can carry. A value's
// static type is followed through pointers, slices, maps, arrays and fields; a value whose static type is an
// interface or a type parameter is followed back to what it could hold: the arguments of every call of the
// function whose parameter it is, the values assigned to the variable or field, the results of the function
// that returned it, and the type arguments of every instantiation. That runs to a fixpoint, so a wrapper of a
// wrapper of json.Unmarshal is an encoder too. What it cannot follow is listed as opaque.
type encodingAnalysis struct {
	engine *engine

	fields map[Key]encoder
	types  map[Key]encoder

	parameters     map[Key]encoder
	typeParameters map[Key]encoder
	methodSinks    map[string]map[int]encoder

	parameterKeys       map[Key]bool
	methodParameters    map[Key]methodParameter
	assignments         map[Key][]valueSite
	fieldAssignments    map[Key][]valueSite
	returns             map[Key]map[int][]valueSite
	literalReturns      map[*ast.FuncLit]map[int][]valueSite
	functionLiterals    map[Key]*ast.FuncLit
	typeParametersOf    map[Key][]Key
	receiverTypeAliases map[Key]Key

	calls          []callSite
	instances      []instanceSite
	functionValues []valueSite

	visitedTypes       map[types.Type]encoder
	visitedExpressions map[siteKey]encoder
	visitedKeys        map[valueKey]encoder
	opaque             map[string]bool

	// reflectNameReaders holds each package variant's answer to readsNamesThroughReflect, once asked.
	reflectNameReaders map[*packages.Package]bool
	// typeNames follows reflect values forward to what reads their type's name.
	typeNames *typeNameFlow
}

type methodParameter struct {
	name  string
	index int
	arity int
}

type callSite struct {
	goPackage *packages.Package
	call      *ast.CallExpr
}

type instanceSite struct {
	goPackage  *packages.Package
	identifier *ast.Ident
	instance   types.Instance
}

// sinkCount sums every encoder bit recorded on parameters, type parameters, method sinks, fields and types,
// so a pass that adds nothing is seen.
func (analysis *encodingAnalysis) sinkCount() int {
	count := 0
	bits := func(set encoder) int {
		total := 0
		for remaining := set; remaining != 0; remaining &= remaining - 1 {
			total++
		}
		return total
	}
	for _, set := range analysis.parameters {
		count += bits(set)
	}
	for _, set := range analysis.typeParameters {
		count += bits(set)
	}
	for _, byIndex := range analysis.methodSinks {
		for _, set := range byIndex {
			count += bits(set)
		}
	}
	for _, set := range analysis.fields {
		count += bits(set)
	}
	for _, set := range analysis.types {
		count += bits(set)
	}
	return count
}

// index records, for one package variant, the calls, instantiations, assignments, returns and parameters.
func (analysis *encodingAnalysis) index(goPackage *packages.Package) {
	engine := analysis.engine
	info := goPackage.TypesInfo
	recordParameters := func(fields *ast.FieldList, method string) {
		if fields == nil {
			return
		}
		index := 0
		arity := 0
		for _, field := range fields.List {
			arity += max(1, len(field.Names))
		}
		for _, field := range fields.List {
			if len(field.Names) == 0 {
				index++
				continue
			}
			for _, name := range field.Names {
				if variable, isVariable := info.Defs[name].(*types.Var); isVariable {
					key := engine.keyer.key(variable)
					analysis.parameterKeys[key] = true
					if method != "" {
						analysis.methodParameters[key] = methodParameter{name: method, index: index, arity: arity}
					}
				}
				index++
			}
		}
	}
	for identifier, instance := range info.Instances {
		analysis.instances = append(
			analysis.instances,
			instanceSite{goPackage: goPackage, identifier: identifier, instance: instance},
		)
	}
	for _, object := range info.Defs {
		switch typed := object.(type) {
		case *types.Func:
			signature := typed.Signature()
			for index := range signature.TypeParams().Len() {
				analysis.typeParametersOf[engine.keyer.key(typed)] = append(
					analysis.typeParametersOf[engine.keyer.key(typed)],
					engine.keyer.key(signature.TypeParams().At(index).Obj()),
				)
			}
			if receiverParameters := signature.RecvTypeParams(); receiverParameters.Len() > 0 {
				receiverType, _ := dereference(signature.Recv().Type())
				if named, isNamed := types.Unalias(receiverType).(*types.Named); isNamed {
					declared := named.Origin().TypeParams()
					for index := range min(receiverParameters.Len(), declared.Len()) {
						analysis.receiverTypeAliases[engine.keyer.key(
							receiverParameters.At(index).Obj(),
						)] = engine.keyer.key(declared.At(index).Obj())
					}
				}
			}
		case *types.TypeName:
			if named, isNamed := typed.Type().(*types.Named); isNamed && !typed.IsAlias() {
				for index := range named.TypeParams().Len() {
					analysis.typeParametersOf[engine.keyer.key(typed)] = append(
						analysis.typeParametersOf[engine.keyer.key(typed)],
						engine.keyer.key(named.TypeParams().At(index).Obj()),
					)
				}
			}
		}
	}
	for _, file := range goPackage.Syntax {
		var functions []ast.Node
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				popped := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if len(functions) > 0 && functions[len(functions)-1] == popped {
					functions = functions[:len(functions)-1]
				}
				return true
			}
			var parent ast.Node
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, node)
			switch typed := node.(type) {
			case *ast.FuncDecl:
				functions = append(functions, typed)
				method := ""
				if typed.Recv != nil {
					method = typed.Name.Name
				}
				recordParameters(typed.Type.Params, method)
			case *ast.FuncLit:
				functions = append(functions, typed)
				recordParameters(typed.Type.Params, "")
			case *ast.CallExpr:
				analysis.calls = append(analysis.calls, callSite{goPackage: goPackage, call: typed})
			case *ast.ReturnStmt:
				if len(functions) == 0 || len(typed.Results) == 0 {
					return true
				}
				var results map[int][]valueSite
				var count int
				switch function := functions[len(functions)-1].(type) {
				case *ast.FuncDecl:
					declared, isFunction := info.Defs[function.Name].(*types.Func)
					if !isFunction {
						return true
					}
					key := engine.keyer.key(declared)
					if analysis.returns[key] == nil {
						analysis.returns[key] = map[int][]valueSite{}
					}
					results = analysis.returns[key]
					count = declared.Signature().Results().Len()
				case *ast.FuncLit:
					if analysis.literalReturns[function] == nil {
						analysis.literalReturns[function] = map[int][]valueSite{}
					}
					results = analysis.literalReturns[function]
					if signature, isSignature := info.TypeOf(function).(*types.Signature); isSignature {
						count = signature.Results().Len()
					}
				}
				if len(typed.Results) == 1 && count > 1 {
					// `return decode()`: each result is the call's.
					for index := range count {
						results[index] = append(
							results[index],
							valueSite{
								goPackage:  goPackage,
								expression: typed.Results[0],
								result:     index,
								where:      typed.Pos(),
							},
						)
					}
					return true
				}
				for index, result := range typed.Results {
					results[index] = append(
						results[index],
						valueSite{goPackage: goPackage, expression: result, where: typed.Pos()},
					)
				}
			case *ast.AssignStmt:
				analysis.indexAssignment(goPackage, typed.Lhs, typed.Rhs, typed.Pos())
			case *ast.ValueSpec:
				var left []ast.Expr
				for _, name := range typed.Names {
					left = append(left, name)
				}
				if len(typed.Values) > 0 {
					analysis.indexAssignment(goPackage, left, typed.Values, typed.Pos())
				}
			case *ast.RangeStmt:
				for _, bound := range []ast.Expr{typed.Key, typed.Value} {
					if identifier, isIdentifier := bound.(*ast.Ident); isIdentifier {
						if key, found := analysis.variableKey(goPackage, identifier); found {
							analysis.assignments[key] = append(
								analysis.assignments[key],
								valueSite{goPackage: goPackage, expression: typed.X, where: typed.Pos()},
							)
						}
					}
				}
			case *ast.CompositeLit:
				analysis.indexCompositeFields(goPackage, typed)
			case *ast.Ident, *ast.SelectorExpr:
				// A function named but not called, handed somewhere as a value. A declaration names no value.
				expression, isExpression := typed.(ast.Expr)
				if !isExpression {
					// Both cases are expressions, so this is never taken; a node that were not one names no value.
					return true
				}
				if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier && info.Uses[identifier] == nil {
					return true
				}
				if call, isCall := parent.(*ast.CallExpr); isCall && ast.Unparen(call.Fun) == expression {
					return true
				}
				if selector, isSelector := parent.(*ast.SelectorExpr); isSelector && selector.Sel == expression {
					return true
				}
				if indexed, isIndex := parent.(*ast.IndexExpr); isIndex && indexed.X == expression {
					return true
				}
				if indexed, isIndex := parent.(*ast.IndexListExpr); isIndex && indexed.X == expression {
					return true
				}
				if _, isFunction := analysis.referencedObject(goPackage, expression).(*types.Func); isFunction {
					analysis.functionValues = append(
						analysis.functionValues,
						valueSite{goPackage: goPackage, expression: expression, where: expression.Pos()},
					)
				}
			}
			return true
		})
	}
}

// referencedObject is what an identifier or selector names.
func (analysis *encodingAnalysis) referencedObject(goPackage *packages.Package, expression ast.Expr) types.Object {
	switch typed := expression.(type) {
	case *ast.Ident:
		if object := goPackage.TypesInfo.Uses[typed]; object != nil {
			return object
		}
		return goPackage.TypesInfo.Defs[typed]
	case *ast.SelectorExpr:
		if selection, found := goPackage.TypesInfo.Selections[typed]; found {
			return selection.Obj()
		}
		return goPackage.TypesInfo.Uses[typed.Sel]
	}
	return nil
}

func (analysis *encodingAnalysis) variableKey(goPackage *packages.Package, identifier *ast.Ident) (Key, bool) {
	variable, isVariable := analysis.referencedObject(goPackage, identifier).(*types.Var)
	if !isVariable || variable.IsField() {
		return Key{}, false
	}
	return analysis.engine.keyer.key(variable), true
}

// indexAssignment records what each left side may now hold: a variable its value, a map or slice element the
// value stored in its container, a field the value stored in it. A tuple assignment records an unseen value.
func (analysis *encodingAnalysis) indexAssignment(
	goPackage *packages.Package,
	left []ast.Expr,
	right []ast.Expr,
	where token.Pos,
) {
	for index, target := range left {
		var value ast.Expr
		result := 0
		switch {
		case len(left) == len(right):
			value = right[index]
		case len(right) == 1:
			// `value, err := decode()`, `value, ok := held.(T)`, `value, ok := table[key]`.
			value = right[0]
			result = index
		}
		target = ast.Unparen(target)
		for {
			indexed, isIndex := target.(*ast.IndexExpr)
			if !isIndex {
				break
			}
			target = ast.Unparen(indexed.X)
		}
		if star, isStar := target.(*ast.StarExpr); isStar {
			target = ast.Unparen(star.X)
		}
		switch typed := target.(type) {
		case *ast.Ident:
			if key, found := analysis.variableKey(goPackage, typed); found {
				if literal, isLiteral := value.(*ast.FuncLit); isLiteral {
					analysis.functionLiterals[key] = literal
				}
				analysis.assignments[key] = append(
					analysis.assignments[key],
					valueSite{goPackage: goPackage, expression: value, result: result, where: where},
				)
			}
		case *ast.SelectorExpr:
			if field, isField := analysis.referencedObject(goPackage, typed).(*types.Var); isField && field.IsField() {
				key := analysis.engine.keyer.key(field)
				analysis.fieldAssignments[key] = append(
					analysis.fieldAssignments[key],
					valueSite{goPackage: goPackage, expression: value, result: result, where: where},
				)
			}
		}
	}
}

// indexCompositeFields records the values a struct literal stores in its fields.
func (analysis *encodingAnalysis) indexCompositeFields(goPackage *packages.Package, literal *ast.CompositeLit) {
	literalType := goPackage.TypesInfo.TypeOf(literal)
	if literalType == nil {
		return
	}
	pointed, _ := dereference(literalType)
	structType, isStruct := pointed.Underlying().(*types.Struct)
	if !isStruct {
		return
	}
	for index, element := range literal.Elts {
		if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
			identifier, isIdentifier := keyValue.Key.(*ast.Ident)
			if !isIdentifier {
				continue
			}
			if field, isField := goPackage.TypesInfo.Uses[identifier].(*types.Var); isField {
				key := analysis.engine.keyer.key(field)
				analysis.fieldAssignments[key] = append(
					analysis.fieldAssignments[key],
					valueSite{goPackage: goPackage, expression: keyValue.Value, where: keyValue.Pos()},
				)
			}
			continue
		}
		if index < structType.NumFields() {
			key := analysis.engine.keyer.key(structType.Field(index))
			analysis.fieldAssignments[key] = append(
				analysis.fieldAssignments[key],
				valueSite{goPackage: goPackage, expression: element, where: element.Pos()},
			)
		}
	}
}

// visitCall reaches every argument a call hands to an encoder or to a parameter known to reach one.
func (analysis *encodingAnalysis) visitCall(site callSite) {
	info := site.goPackage.TypesInfo
	call := site.call
	if value, found := info.Types[call.Fun]; found && value.IsType() {
		return
	}
	function := ast.Unparen(call.Fun)
	switch typed := function.(type) {
	case *ast.IndexExpr:
		function = typed.X
	case *ast.IndexListExpr:
		function = typed.X
	}
	object := analysis.referencedObject(site.goPackage, function)
	var signature *types.Signature
	var parameterKeys []Key
	library := encoder(0)
	var typeNames map[int]encoder
	interfaceMethod := ""
	switch typed := object.(type) {
	case *types.Func:
		declared := typed.Origin()
		signature = declared.Signature()
		library = analysis.encoderOf(declared, site.goPackage)
		typeNames = analysis.typeNameReads(site, declared, signature)
		if receiver := signature.Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
			interfaceMethod = declared.Name()
		}
		for index := range signature.Params().Len() {
			parameterKeys = append(parameterKeys, analysis.engine.keyer.key(signature.Params().At(index)))
		}
	case *types.Var:
		// A call through a variable or a func-typed field calls whatever was stored in it.
		called, isSignature := info.TypeOf(call.Fun).Underlying().(*types.Signature)
		if !isSignature {
			return
		}
		signature = called
		key := analysis.engine.keyer.key(typed)
		stored := analysis.assignments[key]
		if typed.IsField() {
			stored = analysis.fieldAssignments[key]
		}
		var targets [][]Key
		if literal := analysis.functionLiterals[key]; literal != nil {
			targets = append(targets, analysis.literalParameterKeys(site.goPackage, literal))
		}
		for _, value := range stored {
			targets = append(targets, analysis.storedFunctionParameterKeys(value)...)
		}
		analysis.callThroughValue(site, signature, targets)
		return
	default:
		return
	}
	count := signature.Params().Len()
	if count == 0 {
		return
	}
	for index, argument := range call.Args {
		if index >= count && !signature.Variadic() {
			break
		}
		parameterIndex := min(index, count-1)
		parameterType := signature.Params().At(parameterIndex).Type()
		if signature.Variadic() && parameterIndex == count-1 && !call.Ellipsis.IsValid() {
			if slice, isSlice := parameterType.(*types.Slice); isSlice {
				parameterType = slice.Elem()
			}
		}
		set := analysis.parameters[parameterKeys[parameterIndex]] | typeNames[index]
		if library != 0 && isEmptyInterface(parameterType) {
			set |= library
		}
		if interfaceMethod != "" {
			set |= analysis.methodSinks[interfaceMethod][parameterIndex]
		}
		if set != 0 {
			analysis.reach(site.goPackage, argument, set)
		}
	}
}

// typeNameReads is what a call of a function outside the workspace reads of its arguments' type names, by
// argument index: reflect.TypeOf and ValueOf what the reflect value they make reaches, gob.Register and
// RegisterName the value's, a printf-like function what each verb reads.
func (analysis *encodingAnalysis) typeNameReads(
	site callSite,
	function *types.Func,
	signature *types.Signature,
) map[int]encoder {
	if function.Pkg() == nil || analysis.engine.workspacePaths[function.Pkg().Path()] {
		return nil
	}
	switch path := function.Pkg().Path(); {
	case path == "reflect" && (function.Name() == "TypeOf" || function.Name() == "ValueOf"):
		if reads := analysis.typeNames.reads(site.goPackage, site.call); reads != 0 {
			return map[int]encoder{0: reads}
		}
		return nil
	case path == "encoding/gob" && function.Name() == "Register":
		return map[int]encoder{0: encoderGobRegister}
	case path == "encoding/gob" && function.Name() == "RegisterName":
		return map[int]encoder{1: encoderGobRegister}
	}
	return formatTypeNameCall(site.goPackage.TypesInfo, site.call, signature)
}

// encoderOf is a library function's encoder family, as called from caller; a workspace package is never a
// library, whatever its path spells (internal/format/yaml). A reflect call reads a Go name only where the caller
// reads one through reflect or hands reflect's values on to code that might: reflect.ValueOf(value).IsNil() or a
// walk of reflect.TypeOf(value)'s json tags reads none, and a rename cannot change what it sees.
func (analysis *encodingAnalysis) encoderOf(function *types.Func, caller *packages.Package) encoder {
	if function.Pkg() != nil && analysis.engine.workspacePaths[function.Pkg().Path()] {
		return 0
	}
	family := encoderOf(function)
	if family == encoderReflect && !analysis.readsNamesThroughReflect(caller) {
		return 0
	}
	return family
}

// reflectNameMembers are the members of reflect's types that read a Go name: a field's, method's or type's
// name (StructField.Name, Method.Name, Type.Name), a description that spells the type (Type.String, and
// Value.String on anything but a string), and a lookup by name. A package path is no name a rename changes.
var reflectNameMembers = map[string]bool{
	"Name": true, "String": true,
	"FieldByName": true, "FieldByNameFunc": true, "MethodByName": true,
}

// readsNamesThroughReflect reports whether a package variant reads a Go name through reflect: it uses one of
// reflectNameMembers, or a value of one of reflect's types leaves it, as an argument to a function of another
// package or as a result of a function another package can call, so the reading could happen out of sight.
func (analysis *encodingAnalysis) readsNamesThroughReflect(goPackage *packages.Package) bool {
	if reads, known := analysis.reflectNameReaders[goPackage]; known {
		return reads
	}
	reads := false
	for _, object := range goPackage.TypesInfo.Uses {
		if object != nil &&
			object.Pkg() != nil &&
			object.Pkg().Path() == "reflect" &&
			reflectNameMembers[object.Name()] {
			if _, isTypeName := object.(*types.TypeName); !isTypeName {
				reads = true
				break
			}
		}
	}
	for _, file := range goPackage.Syntax {
		if reads {
			break
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if reads {
				return false
			}
			switch typed := node.(type) {
			case *ast.CallExpr:
				callee, isFunction := analysis.referencedObject(goPackage, ast.Unparen(typed.Fun)).(*types.Func)
				if !isFunction ||
					callee.Pkg() == nil ||
					callee.Pkg() == goPackage.Types ||
					callee.Pkg().Path() == "reflect" {
					return true
				}
				for _, argument := range typed.Args {
					if mentionsReflect(goPackage.TypesInfo.TypeOf(argument), map[types.Type]bool{}) {
						reads = true
						return false
					}
				}
			case *ast.FuncDecl:
				declared, isFunction := goPackage.TypesInfo.Defs[typed.Name].(*types.Func)
				if !isFunction || (!declared.Exported() && declared.Signature().Recv() == nil) {
					return true
				}
				results := declared.Signature().Results()
				for index := range results.Len() {
					if mentionsReflect(results.At(index).Type(), map[types.Type]bool{}) {
						reads = true
						return false
					}
				}
			}
			return true
		})
	}
	analysis.reflectNameReaders[goPackage] = reads
	return reads
}

// mentionsReflect reports whether a type is or holds one of reflect's named types, through pointers,
// containers, struct fields and function signatures.
//
// Mutates: seen the types this walk has entered, shared down the recursion so a recursive type ends it
func mentionsReflect(goType types.Type, seen map[types.Type]bool) bool {
	if goType == nil || seen[goType] {
		return false
	}
	seen[goType] = true
	switch typed := types.Unalias(goType).(type) {
	case *types.Named:
		if typed.Obj().Pkg() != nil && typed.Obj().Pkg().Path() == "reflect" {
			return true
		}
		return mentionsReflect(typed.Underlying(), seen)
	case *types.Pointer:
		return mentionsReflect(typed.Elem(), seen)
	case *types.Slice:
		return mentionsReflect(typed.Elem(), seen)
	case *types.Array:
		return mentionsReflect(typed.Elem(), seen)
	case *types.Map:
		return mentionsReflect(typed.Key(), seen) || mentionsReflect(typed.Elem(), seen)
	case *types.Chan:
		return mentionsReflect(typed.Elem(), seen)
	case *types.Struct:
		for index := range typed.NumFields() {
			if mentionsReflect(typed.Field(index).Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		for _, tuple := range []*types.Tuple{typed.Params(), typed.Results()} {
			for index := range tuple.Len() {
				if mentionsReflect(tuple.At(index).Type(), seen) {
					return true
				}
			}
		}
	case *types.Tuple:
		for index := range typed.Len() {
			if mentionsReflect(typed.At(index).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func (analysis *encodingAnalysis) literalParameterKeys(goPackage *packages.Package, literal *ast.FuncLit) []Key {
	signature, isSignature := goPackage.TypesInfo.TypeOf(literal).(*types.Signature)
	if !isSignature {
		return nil
	}
	keys := make([]Key, signature.Params().Len())
	for index := range signature.Params().Len() {
		keys[index] = analysis.engine.keyer.key(signature.Params().At(index))
	}
	return keys
}

// storedFunctionParameterKeys lists the parameters of the function a stored value names: a literal, a
// function, or a method value.
func (analysis *encodingAnalysis) storedFunctionParameterKeys(value valueSite) [][]Key {
	if value.expression == nil {
		return nil
	}
	expression := ast.Unparen(value.expression)
	if literal, isLiteral := expression.(*ast.FuncLit); isLiteral {
		return [][]Key{analysis.literalParameterKeys(value.goPackage, literal)}
	}
	function, isFunction := analysis.referencedObject(value.goPackage, expression).(*types.Func)
	if !isFunction {
		return nil
	}
	signature := function.Origin().Signature()
	keys := make([]Key, signature.Params().Len())
	for index := range signature.Params().Len() {
		keys[index] = analysis.engine.keyer.key(signature.Params().At(index))
	}
	return [][]Key{keys}
}

// callThroughValue reaches each argument of a call through a variable or field with what any function stored
// in it does with that parameter.
func (analysis *encodingAnalysis) callThroughValue(site callSite, signature *types.Signature, targets [][]Key) {
	count := signature.Params().Len()
	for index, argument := range site.call.Args {
		if index >= count && !signature.Variadic() {
			break
		}
		parameterIndex := min(index, count-1)
		set := encoder(0)
		for _, target := range targets {
			if parameterIndex < len(target) {
				set |= analysis.parameters[target[parameterIndex]]
			}
		}
		if set != 0 {
			analysis.reach(site.goPackage, argument, set)
		}
	}
}

func isEmptyInterface(goType types.Type) bool {
	interfaceType, isInterface := goType.Underlying().(*types.Interface)
	return isInterface && interfaceType.NumMethods() == 0 && interfaceType.IsMethodSet()
}

// visitInstance reaches the type arguments of an instantiation whose type parameters reach an encoder, and
// every type argument of an encoder's own generic function (reflect.TypeFor).
func (analysis *encodingAnalysis) visitInstance(site instanceSite) {
	object := site.goPackage.TypesInfo.Uses[site.identifier]
	if object == nil {
		return
	}
	arguments := site.instance.TypeArgs
	if function, isFunction := object.(*types.Func); isFunction {
		library := analysis.encoderOf(function.Origin(), site.goPackage)
		if function.Pkg() != nil && function.Pkg().Path() == "reflect" {
			// reflect.TypeFor[T](): what its reflect.Type reaches reads T's name, or not.
			if call := instantiatingCall(analysis.typeNames, site); call != nil {
				library |= analysis.typeNames.reads(site.goPackage, call)
			} else {
				library |= encoderReflectTypeName
			}
		}
		if library != 0 {
			for index := range arguments.Len() {
				analysis.markType(arguments.At(index), library)
			}
			return
		}
	}
	parameters := analysis.typeParametersOf[analysis.engine.keyer.key(origin(object))]
	for index := range min(len(parameters), arguments.Len()) {
		if set := analysis.typeParameters[parameters[index]]; set != 0 {
			analysis.markType(arguments.At(index), set)
		}
	}
}

// instantiatingCall is the call an instantiated function's identifier is called by, reflect.TypeFor[T](), or nil.
func instantiatingCall(flow *typeNameFlow, site instanceSite) *ast.CallExpr {
	file := fileHolding(site.goPackage, site.identifier.Pos())
	if file == nil {
		return nil
	}
	parents := flow.parentsOf(file)
	var node ast.Node = site.identifier
	for {
		switch parent := parents[node].(type) {
		case *ast.SelectorExpr:
			if parent.Sel != node {
				return nil
			}
			node = parent
			continue
		case *ast.IndexExpr, *ast.IndexListExpr, *ast.ParenExpr:
			node = parent
			continue
		case *ast.CallExpr:
			if ast.Unparen(parent.Fun) == node || parent.Fun == node {
				return parent
			}
		}
		return nil
	}
}

// reach follows one expression handed to an encoder.
func (analysis *encodingAnalysis) reach(goPackage *packages.Package, expression ast.Expr, set encoder) {
	analysis.reachResult(goPackage, expression, 0, set)
}

// reachResult follows one result of an expression: the only one, or the one a tuple assignment took.
func (analysis *encodingAnalysis) reachResult(
	goPackage *packages.Package,
	expression ast.Expr,
	result int,
	set encoder,
) {
	if expression == nil {
		return
	}
	unwrapped := ast.Unparen(expression)
	visit := siteKey{expression: unwrapped, result: result}
	if analysis.visitedExpressions[visit]&set == set {
		return
	}
	analysis.visitedExpressions[visit] |= set
	info := goPackage.TypesInfo
	if result == 0 {
		switch typed := unwrapped.(type) {
		case *ast.UnaryExpr:
			if typed.Op == token.AND {
				analysis.reach(goPackage, typed.X, set)
				return
			}
		case *ast.StarExpr:
			analysis.reach(goPackage, typed.X, set)
			return
		case *ast.CompositeLit:
			for _, element := range typed.Elts {
				if keyValue, isKeyValue := element.(*ast.KeyValueExpr); isKeyValue {
					if _, isStructKey := info.Uses[identifierOf(keyValue.Key)].(*types.Var); !isStructKey {
						analysis.reach(goPackage, keyValue.Key, set)
					}
					analysis.reach(goPackage, keyValue.Value, set)
					continue
				}
				analysis.reach(goPackage, element, set)
			}
		}
	}
	staticType := info.TypeOf(unwrapped)
	if tuple, isTuple := staticType.(*types.Tuple); isTuple {
		if result >= tuple.Len() {
			return
		}
		staticType = tuple.At(result).Type()
	} else if result > 0 {
		// The ok of a comma-ok form.
		return
	}
	if staticType == nil {
		return
	}
	analysis.markType(staticType, set)
	if !holdsUnseenValues(staticType) {
		return
	}
	switch typed := unwrapped.(type) {
	case *ast.CompositeLit:
		return
	case *ast.Ident:
		object := analysis.referencedObject(goPackage, typed)
		variable, isVariable := object.(*types.Var)
		if !isVariable {
			if _, isNil := object.(*types.Nil); isNil {
				return
			}
			analysis.markOpaque(goPackage, unwrapped, "an identifier that is no variable")
			return
		}
		key := analysis.engine.keyer.key(variable)
		if analysis.parameterKeys[key] {
			analysis.parameters[key] |= set
		}
		analysis.reachValues(valueKey{key: key}, analysis.assignments[key], set)
		return
	case *ast.CallExpr:
		if value, found := info.Types[typed.Fun]; found && value.IsType() {
			if len(typed.Args) == 1 {
				analysis.reach(goPackage, typed.Args[0], set)
			}
			return
		}
		switch callee := analysis.referencedObject(goPackage, ast.Unparen(typed.Fun)).(type) {
		case *types.Builtin:
			switch callee.Name() {
			case "append":
				for _, argument := range typed.Args {
					analysis.reach(goPackage, argument, set)
				}
				return
			case "make", "new":
				// Empty: what it holds is stored later, and indexed as an assignment.
				return
			}
		case *types.Func:
			key := analysis.engine.keyer.key(callee.Origin())
			if returns, found := analysis.returns[key]; found {
				analysis.reachValues(valueKey{key: key, result: result}, returns[result], set)
				return
			}
		case *types.Var:
			if literal := analysis.functionLiterals[analysis.engine.keyer.key(callee)]; literal != nil {
				analysis.reachValues(
					valueKey{literal: literal, result: result},
					analysis.literalReturns[literal][result],
					set,
				)
				return
			}
		}
		analysis.markOpaque(goPackage, unwrapped, "the result of a call the analysis does not follow")
		return
	case *ast.TypeAssertExpr:
		analysis.reach(goPackage, typed.X, set)
		return
	case *ast.SelectorExpr:
		if field, isField := analysis.referencedObject(goPackage, typed).(*types.Var); isField {
			key := analysis.engine.keyer.key(field)
			if field.IsField() {
				analysis.reachValues(valueKey{key: key}, analysis.fieldAssignments[key], set)
			} else {
				analysis.reachValues(valueKey{key: key}, analysis.assignments[key], set)
			}
			return
		}
	case *ast.IndexExpr:
		analysis.reach(goPackage, typed.X, set)
		return
	case *ast.SliceExpr:
		analysis.reach(goPackage, typed.X, set)
		return
	case *ast.BasicLit:
		return
	}
	analysis.markOpaque(goPackage, unwrapped, "a value the analysis does not follow")
}

func identifierOf(expression ast.Expr) *ast.Ident {
	identifier, _ := expression.(*ast.Ident)
	return identifier
}

// valueKey names one set of values: what a variable or field holds, or one result of a function.
type valueKey struct {
	key     Key
	literal *ast.FuncLit
	result  int
}

// reachValues reaches every value a variable, field or function result may hold.
func (analysis *encodingAnalysis) reachValues(identity valueKey, values []valueSite, set encoder) {
	if analysis.visitedKeys[identity]&set == set {
		return
	}
	analysis.visitedKeys[identity] |= set
	for _, value := range values {
		analysis.reachResult(value.goPackage, value.expression, value.result, set)
	}
}

func (analysis *encodingAnalysis) markOpaque(goPackage *packages.Package, expression ast.Expr, why string) {
	position := goPackage.Fset.Position(expression.Pos())
	analysis.opaque[fmt.Sprintf(
		"%s: %s (%s)",
		analysis.engine.relativePosition(positionString(position)),
		types.ExprString(expression),
		why,
	)] = true
}

// holdsUnseenValues reports whether a value's static type leaves part of what it holds to an interface or a
// type parameter, at its top or inside an unnamed container.
func holdsUnseenValues(goType types.Type) bool {
	switch typed := types.Unalias(goType).(type) {
	case *types.Interface, *types.TypeParam:
		return true
	case *types.Named:
		return types.IsInterface(typed)
	case *types.Pointer:
		return holdsUnseenValues(typed.Elem())
	case *types.Slice:
		return holdsUnseenValues(typed.Elem())
	case *types.Array:
		return holdsUnseenValues(typed.Elem())
	case *types.Map:
		return holdsUnseenValues(typed.Key()) || holdsUnseenValues(typed.Elem())
	case *types.Chan:
		return holdsUnseenValues(typed.Elem())
	}
	return false
}

// markType records every field and named type a value of goType carries, and follows the values stored in any
// field whose type leaves them unseen.
func (analysis *encodingAnalysis) markType(goType types.Type, set encoder) {
	if analysis.visitedTypes[goType]&set == set {
		return
	}
	analysis.visitedTypes[goType] |= set
	switch typed := goType.(type) {
	case *types.Alias:
		analysis.markType(types.Unalias(typed), set)
	case *types.Named:
		// A held type's name is not its own; %T of a named type spells its name and nothing it holds.
		analysis.types[analysis.engine.keyer.key(typed.Origin().Obj())] |= set &^ encoderHeldTypeName
		analysis.markType(typed.Underlying(), set&^encoderFormatType)
	case *types.Pointer:
		analysis.markType(typed.Elem(), set)
	case *types.Slice:
		analysis.markType(typed.Elem(), heldLevel(set))
	case *types.Array:
		analysis.markType(typed.Elem(), heldLevel(set))
	case *types.Map:
		analysis.markType(typed.Key(), heldLevel(set))
		analysis.markType(typed.Elem(), heldLevel(set))
	case *types.Chan:
		analysis.markType(typed.Elem(), heldLevel(set))
	case *types.Struct:
		for index := range typed.NumFields() {
			field := typed.Field(index)
			key := analysis.engine.keyer.key(field)
			analysis.fields[key] |= set
			analysis.markType(field.Type(), heldLevel(set))
			if holdsUnseenValues(field.Type()) {
				// %T spells an interface field's static type, never what it holds.
				analysis.reachValues(
					valueKey{key: key},
					analysis.fieldAssignments[key],
					heldLevel(set)&^encoderFormatType,
				)
			}
		}
	case *types.TypeParam:
		key := analysis.engine.keyer.key(typed.Obj())
		analysis.typeParameters[key] |= set
		if declared, isReceiver := analysis.receiverTypeAliases[key]; isReceiver {
			analysis.typeParameters[declared] |= set
		}
	}
}

// heldLevel is a set as it reaches what a type holds: a reflect value of a held type reads the name of what is
// held there.
func heldLevel(set encoder) encoder {
	if set&encoderHeldTypeName == 0 {
		return set
	}
	return set&^encoderHeldTypeName | encoderReflectTypeName
}

// checkFunctionValue lists an encoder, or a function known to reach one, handed somewhere as a value: its
// calls are out of sight.
func (analysis *encodingAnalysis) checkFunctionValue(value valueSite) {
	function, isFunction := analysis.referencedObject(value.goPackage, value.expression).(*types.Func)
	if !isFunction {
		return
	}
	declared := function.Origin()
	reaches := analysis.encoderOf(declared, value.goPackage) != 0
	signature := declared.Signature()
	for index := range signature.Params().Len() {
		if analysis.parameters[analysis.engine.keyer.key(signature.Params().At(index))] != 0 {
			reaches = true
		}
	}
	if reaches {
		analysis.markOpaque(value.goPackage, value.expression, "a function that reaches an encoder, used as a value")
	}
}

// fieldDeclaration is where a struct field is spelled.
type fieldDeclaration struct {
	goPackage *packages.Package
	field     *ast.Field
	structure *ast.StructType
}

func tagOf(field *ast.Field) reflect.StructTag {
	if field.Tag == nil {
		return ""
	}
	value := field.Tag.Value
	if len(value) >= 2 {
		value = value[1 : len(value)-1]
	}
	return reflect.StructTag(value)
}

// namesKey reports whether a tag gives this encoder a key, or tells it to skip the field.
func namesKey(tag reflect.StructTag, name string) bool {
	value, present := tag.Lookup(name)
	if !present {
		return false
	}
	key, _, _ := strings.Cut(value, ",")
	return key != ""
}
