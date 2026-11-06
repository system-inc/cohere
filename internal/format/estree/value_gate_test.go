package estree_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The gate holds format to one promise of the typed property values: an estree value never travels as an
// empty interface. A property holds a String where it held a string, so code that keeps a value in an `any`
// and type-switches it later (`case string:`) falls to its default branch and answers differently with no
// build error. The compiler catches every assertion on a Value; this catches the conversions it cannot,
// before they reach a switch.
//
// A conversion is found wherever Go makes one implicitly or is asked to: a call's argument, variadic ones and
// a tuple handed on whole included, an explicit conversion, an assignment, a typed var, a return, a composite
// literal's element, key or field, a send, a map index, and a comparison with an interface. A value printed
// by fmt, or by testing's methods that format through it, is let pass, since printing reads it the same.

// valueMarker is the unexported method that makes a type a Value, and the package that declares it.
const valueMarker = "isValue"

const estreePackagePath = "github.com/system-inc/cohere/internal/format/estree"

// gateLoadMode is what the scan reads: each package's syntax and the types of its expressions, its imports'
// types coming from export data.
const gateLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
	packages.NeedTypesInfo

// testingPrinters are testing's methods that format their arguments with fmt.
var testingPrinters = []string{"Error", "Errorf", "Fatal", "Fatalf", "Log", "Logf", "Skip", "Skipf"}

func TestNoValueIsConvertedToAnEmptyInterface(test *testing.T) {
	test.Parallel()

	loaded := loadForGate(test, "..", "./...")
	loadedEstree, declared := false, false
	files := 0
	for _, loadedPackage := range loaded {
		files += len(loadedPackage.Syntax)
		if loadedPackage.PkgPath == estreePackagePath {
			loadedEstree = true
			declared = declared || loadedPackage.Types.Scope().Lookup("Value") != nil
		}
	}
	if !loadedEstree {
		test.Fatalf("loading internal/format found %d packages and estree not among them", len(loaded))
	}
	test.Logf("scanned %d files in %d packages, tests included", files, len(loaded))
	if !declared {
		test.Fatal("estree declares no Value, so the gate would pass finding nothing to look for")
	}
	for _, found := range valueConversions(loaded, estreePackagePath) {
		test.Errorf("%s: %s", found.position, found.description)
	}
}

// The gate is only as good as its reach, so the fixture plants a conversion of its own Value in each way the
// gate looks, marks each line, and the gate must name those lines and no other.
func TestTheGateFindsEveryPlantedConversion(test *testing.T) {
	test.Parallel()

	loaded := loadForGate(test, ".", "./testdata/valuegate")
	fixturePath := filepath.Join("testdata", "valuegate", "valuegate.go")
	contents, readError := os.ReadFile(fixturePath)
	if readError != nil {
		test.Fatal(readError)
	}
	var planted []int
	for index, line := range strings.Split(string(contents), "\n") {
		if strings.HasSuffix(line, "// planted") {
			planted = append(planted, index+1)
		}
	}
	if len(planted) == 0 {
		test.Fatalf("%s plants no conversion, so the gate would pass finding none", fixturePath)
	}
	var found []int
	for _, conversion := range valueConversions(loaded, loaded[0].PkgPath) {
		found = append(found, conversion.position.Line)
	}
	if !slices.Equal(found, planted) {
		test.Errorf("planted conversions on lines %v, and the gate found them on %v", planted, found)
	}
}

// loadForGate loads the packages the patterns name, tests included, from directory, and fails on any error,
// since a package that did not type-check would hide its conversions.
func loadForGate(test *testing.T, directory string, patterns ...string) []*packages.Package {
	loaded, loadError := packages.Load(
		&packages.Config{Mode: gateLoadMode, Dir: directory, Tests: true},
		patterns...,
	)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded) == 0 {
		test.Fatalf("loading %v found no packages", patterns)
	}
	for _, loadedPackage := range loaded {
		for _, problem := range loadedPackage.Errors {
			test.Errorf("loading %s: %v", loadedPackage.ID, problem)
		}
	}
	if test.Failed() {
		test.FailNow()
	}
	return loaded
}

// conversion is one Value converted to an empty interface: where, and what into what.
type conversion struct {
	position    token.Position
	description string
}

// valueConversions are the conversions of a Value of the package at valuePath to an empty interface across
// the loaded packages, each once though a file is loaded with and without its tests, in file and line order.
func valueConversions(loaded []*packages.Package, valuePath string) []conversion {
	found := map[token.Position]string{}
	for _, loadedPackage := range loaded {
		gate := &valueGate{
			information: loadedPackage.TypesInfo,
			files:       loadedPackage.Fset,
			valuePath:   valuePath,
			found:       found,
		}
		for _, file := range loadedPackage.Syntax {
			var enclosing []ast.Node
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					enclosing = enclosing[:len(enclosing)-1]
					return true
				}
				gate.check(node, enclosing)
				enclosing = append(enclosing, node)
				return true
			})
		}
	}
	conversions := make([]conversion, 0, len(found))
	for position, description := range found {
		conversions = append(conversions, conversion{position: position, description: description})
	}
	slices.SortFunc(conversions, func(left conversion, right conversion) int {
		if order := strings.Compare(left.position.Filename, right.position.Filename); order != 0 {
			return order
		}
		if left.position.Line != right.position.Line {
			return left.position.Line - right.position.Line
		}
		return left.position.Column - right.position.Column
	})
	return conversions
}

// isEmptyInterface is whether a value of the type holds anything at all: `any`, `interface{}`, or a named
// type over one. A type parameter is not one, since a Value given for it is not boxed.
func isEmptyInterface(target types.Type) bool {
	if _, isParameter := target.(*types.TypeParam); isParameter {
		return false
	}
	underlying, isInterface := target.Underlying().(*types.Interface)
	return isInterface && underlying.Empty()
}

// isValueType is whether a value of the type is a Value: the package's Value interface, or a type whose method
// set holds the marker, which only the package's own types can, since the marker is unexported.
func isValueType(valueType types.Type, valuePath string) bool {
	base := valueType
	if pointer, isPointer := base.(*types.Pointer); isPointer {
		base = pointer.Elem()
	}
	named, isNamed := base.(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != valuePath {
		return false
	}
	return types.NewMethodSet(valueType).Lookup(named.Obj().Pkg(), valueMarker) != nil
}

// isPrinter is whether the call is fmt's, or one of testing's methods that format through fmt.
func isPrinter(information *types.Info, call *ast.CallExpr) bool {
	var name *ast.Ident
	switch callee := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		name = callee
	case *ast.SelectorExpr:
		name = callee.Sel
	default:
		return false
	}
	function, isFunction := information.Uses[name].(*types.Func)
	if !isFunction || function.Pkg() == nil {
		return false
	}
	switch function.Pkg().Path() {
	case "fmt":
		return true
	case "testing":
		return slices.Contains(testingPrinters, function.Name())
	}
	return false
}

// tupleOrTypes are the types of values as they are handed on: one tuple handed whole to several targets is
// its elements, and each expression otherwise its own type.
func tupleOrTypes(information *types.Info, values []ast.Expr, targets int) ([]ast.Expr, []types.Type) {
	if len(values) == 1 && targets > 1 {
		if tuple, isTuple := information.TypeOf(values[0]).(*types.Tuple); isTuple {
			expressions := make([]ast.Expr, tuple.Len())
			valueTypes := make([]types.Type, tuple.Len())
			for index := range tuple.Len() {
				expressions[index] = values[0]
				valueTypes[index] = tuple.At(index).Type()
			}
			return expressions, valueTypes
		}
	}
	valueTypes := make([]types.Type, len(values))
	for index, value := range values {
		valueTypes[index] = information.TypeOf(value)
	}
	return values, valueTypes
}

// tupleTypes are a tuple's element types.
func tupleTypes(tuple *types.Tuple) []types.Type {
	elementTypes := make([]types.Type, tuple.Len())
	for index := range tuple.Len() {
		elementTypes[index] = tuple.At(index).Type()
	}
	return elementTypes
}

// enclosingSignature is the signature of the innermost function around a return, a literal or a declaration.
func enclosingSignature(information *types.Info, enclosing []ast.Node) *types.Signature {
	for index := len(enclosing) - 1; index >= 0; index-- {
		switch function := enclosing[index].(type) {
		case *ast.FuncLit:
			signature, _ := information.TypeOf(function).(*types.Signature)
			return signature
		case *ast.FuncDecl:
			if object := information.Defs[function.Name]; object != nil {
				signature, _ := object.Type().(*types.Signature)
				return signature
			}
			return nil
		}
	}
	return nil
}

// parameterTypes are the types the call's arguments are converted to, the variadic tail spread over its
// elements unless the call hands on a slice with `...`.
func parameterTypes(signature *types.Signature, call *ast.CallExpr, arguments int) []types.Type {
	parameters := signature.Params()
	targets := make([]types.Type, 0, arguments)
	for index := range arguments {
		switch {
		case signature.Variadic() && index >= parameters.Len()-1:
			last := parameters.At(parameters.Len() - 1).Type()
			if call.Ellipsis.IsValid() {
				targets = append(targets, last)
				continue
			}
			slice, isSlice := last.Underlying().(*types.Slice)
			if !isSlice {
				// append's string-to-bytes form, append(bytes, text...), which converts nothing.
				targets = append(targets, nil)
				continue
			}
			targets = append(targets, slice.Elem())
		case index < parameters.Len():
			targets = append(targets, parameters.At(index).Type())
		default:
			targets = append(targets, nil)
		}
	}
	return targets
}

// structFieldType is the type of the field a composite literal's element sets, by its key or its place.
func structFieldType(structure *types.Struct, element ast.Expr, place int) types.Type {
	if keyed, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
		name, isName := keyed.Key.(*ast.Ident)
		if !isName {
			return nil
		}
		for index := range structure.NumFields() {
			if structure.Field(index).Name() == name.Name {
				return structure.Field(index).Type()
			}
		}
		return nil
	}
	if place < structure.NumFields() {
		return structure.Field(place).Type()
	}
	return nil
}

// valueOf is a composite literal element's value, past its key.
func valueOf(element ast.Expr) ast.Expr {
	if keyed, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
		return keyed.Value
	}
	return element
}

// underlyingOf is a type's underlying type, and nil for an expression go/types recorded no type for.
func underlyingOf(recorded types.Type) types.Type {
	if recorded == nil {
		return nil
	}
	return recorded.Underlying()
}

// describe is a type as the finding names it, qualified by package name.
func describe(described types.Type) string {
	return types.TypeString(described, func(owner *types.Package) string { return owner.Name() })
}

// valueGate walks one package's files for conversions, recording each into found by its position.
type valueGate struct {
	information *types.Info
	files       *token.FileSet
	valuePath   string
	found       map[token.Position]string
}

// check looks at one node for the conversions it makes, enclosing being the nodes around it, innermost last.
func (gate *valueGate) check(node ast.Node, enclosing []ast.Node) {
	information := gate.information
	switch typed := node.(type) {
	case *ast.CallExpr:
		gate.checkCall(typed)
	case *ast.AssignStmt:
		if typed.Tok != token.ASSIGN && typed.Tok != token.DEFINE {
			return
		}
		// A name := defines takes its value's type and converts nothing; one it redeclares keeps its own.
		targets := make([]types.Type, len(typed.Lhs))
		for index, left := range typed.Lhs {
			targets[index] = information.TypeOf(left)
		}
		gate.checkAll(typed.Rhs, targets, "assigned")
	case *ast.ValueSpec:
		if typed.Type == nil || len(typed.Values) == 0 {
			return
		}
		target := information.TypeOf(typed.Type)
		targets := make([]types.Type, len(typed.Names))
		for index := range targets {
			targets[index] = target
		}
		gate.checkAll(typed.Values, targets, "declared")
	case *ast.ReturnStmt:
		signature := enclosingSignature(information, enclosing)
		if signature == nil || len(typed.Results) == 0 {
			return
		}
		gate.checkAll(typed.Results, tupleTypes(signature.Results()), "returned")
	case *ast.CompositeLit:
		gate.checkComposite(typed)
	case *ast.SendStmt:
		if channel, isChannel := underlyingOf(information.TypeOf(typed.Chan)).(*types.Chan); isChannel {
			gate.convert(typed.Value, information.TypeOf(typed.Value), channel.Elem(), "sent")
		}
	case *ast.IndexExpr:
		if table, isMap := underlyingOf(information.TypeOf(typed.X)).(*types.Map); isMap {
			gate.convert(typed.Index, information.TypeOf(typed.Index), table.Key(), "a map's index")
		}
	case *ast.BinaryExpr:
		if typed.Op != token.EQL && typed.Op != token.NEQ {
			return
		}
		leftType, rightType := information.TypeOf(typed.X), information.TypeOf(typed.Y)
		gate.convert(typed.Y, rightType, leftType, "compared")
		gate.convert(typed.X, leftType, rightType, "compared")
	}
}

// checkCall checks an explicit conversion's operand, or a call's arguments against its parameters. A printing
// call is let pass.
func (gate *valueGate) checkCall(call *ast.CallExpr) {
	information := gate.information
	callee := information.Types[ast.Unparen(call.Fun)]
	if callee.IsType() {
		if len(call.Args) == 1 {
			gate.convert(call.Args[0], information.TypeOf(call.Args[0]), callee.Type, "converted explicitly")
		}
		return
	}
	if callee.Type == nil || isPrinter(information, call) {
		return
	}
	// A builtin's recorded type is the signature of this one call, so append is read like any function.
	signature, isSignature := callee.Type.Underlying().(*types.Signature)
	if !isSignature {
		return
	}
	arguments := len(call.Args)
	if arguments == 1 {
		if tuple, isTuple := information.TypeOf(call.Args[0]).(*types.Tuple); isTuple {
			arguments = tuple.Len()
		}
	}
	gate.checkAll(call.Args, parameterTypes(signature, call, arguments), "passed")
}

// checkComposite checks a composite literal's elements against its fields, elements or keys.
func (gate *valueGate) checkComposite(literal *ast.CompositeLit) {
	information := gate.information
	literalType := information.TypeOf(literal)
	if literalType == nil {
		return
	}
	if pointer, isPointer := literalType.Underlying().(*types.Pointer); isPointer {
		literalType = pointer.Elem()
	}
	switch underlying := literalType.Underlying().(type) {
	case *types.Struct:
		for place, element := range literal.Elts {
			value := valueOf(element)
			gate.convert(value, information.TypeOf(value), structFieldType(underlying, element, place), "a field")
		}
	case *types.Slice:
		gate.checkElements(literal.Elts, underlying.Elem())
	case *types.Array:
		gate.checkElements(literal.Elts, underlying.Elem())
	case *types.Map:
		for _, element := range literal.Elts {
			keyed, isKeyed := element.(*ast.KeyValueExpr)
			if !isKeyed {
				continue
			}
			gate.convert(keyed.Key, information.TypeOf(keyed.Key), underlying.Key(), "a map's key")
			gate.convert(keyed.Value, information.TypeOf(keyed.Value), underlying.Elem(), "a map's value")
		}
	}
}

// checkElements checks a slice or array literal's elements against its element type.
func (gate *valueGate) checkElements(elements []ast.Expr, elementType types.Type) {
	for _, element := range elements {
		value := valueOf(element)
		gate.convert(value, gate.information.TypeOf(value), elementType, "an element")
	}
}

// checkAll checks values against the targets they are handed to, a tuple spread over several.
func (gate *valueGate) checkAll(values []ast.Expr, targets []types.Type, how string) {
	expressions, valueTypes := tupleOrTypes(gate.information, values, len(targets))
	for index, expression := range expressions {
		if index < len(targets) {
			gate.convert(expression, valueTypes[index], targets[index], how)
		}
	}
}

// convert records the expression when a Value is converted to an empty interface there.
func (gate *valueGate) convert(expression ast.Expr, from types.Type, into types.Type, how string) {
	if from == nil || into == nil || !isEmptyInterface(into) || !isValueType(from, gate.valuePath) {
		return
	}
	position := gate.files.Position(expression.Pos())
	gate.found[position] = fmt.Sprintf("a %s %s as %s", describe(from), how, describe(into))
}
