package rules

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// rulesPackagePath is this package's import path, under which every rule's own package lies.
const rulesPackagePath = "github.com/system-inc/cohere/internal/go_language/rules"

// The scope questions the rules' code asks today, by form: 18 calls of LookupParent and 8 of Lookup on what a call of
// Scope returns. Each count is held exactly, so a branch of asksScope that stops seeing its form fails the guard.
const (
	lookupParentQuestions = 18
	scopeLookupQuestions  = 8
)

// scopeQuestion is one place the rules' code asks a scope about a name: its file and line under the rules' directory,
// its package's import path, the method it calls, LookupParent or Lookup, and its position as parsed, by which the
// typed load finds the call again (scopeAsksOf).
type scopeQuestion struct {
	place       string
	packagePath string
	method      string
	position    token.Position
}

// scopeQuestionsIn are every scope question of the code under directory, the rules' own, by the syntax: a call of
// LookupParent, and of Lookup on what a call of Scope returns, `pkg.Scope().Lookup(name)`, in every Go file that is no
// test and lies in no testdata, a fixture's, whatever its build constraints. A lookup in unsafe's scope,
// `types.Unsafe.Scope()`, asks a package every build holds alike, as the universe's does, and is no question. Each
// import path is the module's, rulesPackagePath, joined with the file's directory.
func scopeQuestionsIn(test *testing.T, directory string) []scopeQuestion {
	test.Helper()
	var questions []scopeQuestion
	walkError := filepath.WalkDir(directory, func(file string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			return nil
		}
		absolute, absoluteError := filepath.Abs(file)
		if absoluteError != nil {
			return absoluteError
		}
		fileSet := token.NewFileSet()
		parsed, parseError := parser.ParseFile(fileSet, absolute, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		relative, relativeError := filepath.Rel(directory, filepath.Dir(file))
		if relativeError != nil {
			return relativeError
		}
		packagePath := path.Join(rulesPackagePath, filepath.ToSlash(relative))
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			method := asksScope(call)
			if method == "" {
				return true
			}
			position := fileSet.Position(call.Pos())
			// Ignored: file lies under directory, the walk's own root, so its place there is always found.
			place, _ := filepath.Rel(directory, file)
			questions = append(questions, scopeQuestion{place: fmt.Sprintf("%s:%d", filepath.ToSlash(place),
				position.Line), packagePath: packagePath, method: method, position: position})
			return true
		})
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return questions
}

// asksScope is the method by which call asks a scope about a name, LookupParent or Lookup, and empty for a call that
// asks none (scopeQuestionsIn).
func asksScope(call *ast.CallExpr) string {
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return ""
	}
	switch selector.Sel.Name {
	case "LookupParent":
		return "LookupParent"
	case "Lookup":
		scopeCall, isCall := selector.X.(*ast.CallExpr)
		if !isCall {
			return ""
		}
		scope, isScope := scopeCall.Fun.(*ast.SelectorExpr)
		if !isScope || scope.Sel.Name != "Scope" {
			return ""
		}
		owner, isOwner := scope.X.(*ast.SelectorExpr)
		if !isOwner || owner.Sel.Name != "Unsafe" {
			return "Lookup"
		}
		if qualifier, isQualifier := owner.X.(*ast.Ident); isQualifier && qualifier.Name == "types" {
			return ""
		}
		return "Lookup"
	}
	return ""
}

// functionPackage is the import path of the package function, a func value, is declared in, by its symbol's name, as
// the runtime names it: `path/to/package.function`, `path/to/package.(*type).method` or `path/to/package.init.func1`.
// Empty for a nil func.
func functionPackage(function reflect.Value) string {
	if !function.IsValid() || function.IsNil() {
		return ""
	}
	symbol := runtime.FuncForPC(function.Pointer()).Name()
	lastSlash := strings.LastIndex(symbol, "/")
	dot := strings.Index(symbol[lastSlash+1:], ".")
	if dot < 0 {
		return symbol
	}
	return symbol[:lastSlash+1+dot]
}

// rulePackagesOf are the rule packages, every package under rulesPackagePath that root, this package as loaded,
// imports from its code that is no test: the packages the registry wires a rule from, each a rule's own.
func rulePackagesOf(root *packages.Package) map[string]bool {
	rulePackages := map[string]bool{}
	for importedPath := range root.Imports {
		if strings.HasPrefix(importedPath, rulesPackagePath+"/") {
			rulePackages[importedPath] = true
		}
	}
	return rulePackages
}

// rulesByPackage are the names of registered's rules by the rule package (rulePackagesOf) whose source holds their
// code, whatever wires it: wired's, the packages the registry's own tables name for each rule (registryWiring), and
// the package of each func a rule runs that lies in a rule package, its analyzer's Run and every callback of its
// listener, a listener of the representation's among them. A rule a dispatcher runs through closures another package
// builds, as staticcheck_call.FunctionCheck builds them, is found by the registry's table, its funcs being the
// dispatcher's and the builder's.
func rulesByPackage(
	registered []rule.Rule,
	rulePackages map[string]bool,
	wired map[string][]string,
) map[string][]string {
	byPackage := map[string][]string{}
	add := func(packagePath string, ruleName string) {
		if rulePackages[packagePath] && !slices.Contains(byPackage[packagePath], ruleName) {
			byPackage[packagePath] = append(byPackage[packagePath], ruleName)
		}
	}
	for packagePath, ruleNames := range wired {
		for _, ruleName := range ruleNames {
			add(packagePath, ruleName)
		}
	}
	addCallbacks := func(ruleName string, callbacks reflect.Value) {
		for index := range callbacks.NumField() {
			if field := callbacks.Field(index); field.Kind() == reflect.Func {
				add(functionPackage(field), ruleName)
			}
		}
	}
	for _, registeredRule := range registered {
		if registeredRule.Analyzer != nil {
			add(functionPackage(reflect.ValueOf(registeredRule.Analyzer.Run)), registeredRule.Name)
		}
		if registeredRule.Listener == nil {
			continue
		}
		addCallbacks(registeredRule.Name, reflect.ValueOf(*registeredRule.Listener))
		if representation := registeredRule.Listener.IntermediateRepresentation; representation != nil {
			addCallbacks(registeredRule.Name, reflect.ValueOf(*representation))
		}
	}
	for _, ruleNames := range byPackage {
		slices.Sort(ruleNames)
	}
	return byPackage
}

// registryWiring are the rules the registry's tables wire from each rule package, read from root's syntax, this
// package as loaded: each entry of ports, by the packages its value names, its rules those registered runs with the
// entry's analyzer; and each element of House's list, by the packages it names, its rule the one House returns there.
// A table the registry no longer spells this way fails, so the guard never reads it as wiring nothing.
func registryWiring(root *packages.Package, registered []rule.Rule) (map[string][]string, []string) {
	wired := map[string][]string{}
	var problems []string
	add := func(element ast.Expr, ruleName string) {
		ast.Inspect(element, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			if imported, isImported := root.TypesInfo.Uses[identifier].(*types.PkgName); isImported &&
				!slices.Contains(wired[imported.Imported().Path()], ruleName) {
				wired[imported.Imported().Path()] = append(wired[imported.Imported().Path()], ruleName)
			}
			return true
		})
	}
	readPorts, readHouse := false, false
	for _, file := range root.Syntax {
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					value, isValue := specification.(*ast.ValueSpec)
					if !isValue || len(value.Names) != 1 || value.Names[0].Name != "ports" || len(value.Values) != 1 {
						continue
					}
					table, isTable := value.Values[0].(*ast.CompositeLit)
					if !isTable {
						continue
					}
					readPorts = true
					for _, element := range table.Elts {
						entry, isEntry := element.(*ast.KeyValueExpr)
						key := root.TypesInfo.Types[entry.Key].Value
						if !isEntry || key == nil || key.Kind() != constant.String {
							problems = append(problems, fmt.Sprintf("ports holds %s, which is no entry under a "+
								"constant name", types.ExprString(element)))
							continue
						}
						original := ports[constant.StringVal(key)].analyzer
						for _, registeredRule := range registered {
							if registeredRule.Analyzer == original {
								add(entry.Value, registeredRule.Name)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if declaration.Name.Name != "House" || declaration.Recv != nil || declaration.Body == nil {
					continue
				}
				house := House()
				for _, statement := range declaration.Body.List {
					returned, isReturn := statement.(*ast.ReturnStmt)
					if !isReturn || len(returned.Results) != 1 {
						continue
					}
					list, isList := returned.Results[0].(*ast.CompositeLit)
					if !isList || len(list.Elts) != len(house) {
						continue
					}
					readHouse = true
					for index, element := range list.Elts {
						add(element, house[index].Name)
					}
				}
			}
		}
	}
	if !readPorts {
		problems = append(problems, "the registry's ports table is no `var ports = map[string]port{...}`, so the "+
			"guard cannot tell which rule package holds each port's code")
	}
	if !readHouse {
		problems = append(problems, "House returns no list of one element per house rule, so the guard cannot tell "+
			"which rule package holds each house rule's code")
	}
	return wired, problems
}

// reachingRulePackages are the rule packages (rulePackagesOf) whose code reaches each package of graph, by its import
// path: a rule package that imports it, directly or through any package between, which need not lie under the rules'
// directory. A rule package reaches its own imports, never itself.
func reachingRulePackages(graph map[string]*packages.Package, rulePackages map[string]bool) map[string][]string {
	reaching := map[string][]string{}
	for _, rulePackage := range slices.Sorted(maps.Keys(rulePackages)) {
		start, isLoaded := graph[rulePackage]
		if !isLoaded {
			continue
		}
		seen := map[string]bool{}
		pending := slices.Collect(maps.Values(start.Imports))
		for len(pending) > 0 {
			next := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if seen[next.PkgPath] {
				continue
			}
			seen[next.PkgPath] = true
			reaching[next.PkgPath] = append(reaching[next.PkgPath], rulePackage)
			pending = append(pending, slices.Collect(maps.Values(next.Imports))...)
		}
	}
	return reaching
}

// scopeNames are the names one scope question asks, as its argument resolves by the types (scopeNameResolver):
// constant names, prefixes it numbers, each followed by a decimal integer, whether it asks the name of an identifier
// the body uses, and whether it asks an import's own name.
type scopeNames struct {
	names      []string
	prefixes   []string
	uses       bool
	importName bool
}

// mergedScopeNames are what both first and second ask.
func mergedScopeNames(first scopeNames, second scopeNames) scopeNames {
	return scopeNames{names: append(slices.Clone(first.names), second.names...),
		prefixes: append(slices.Clone(first.prefixes), second.prefixes...), uses: first.uses || second.uses,
		importName: first.importName || second.importName}
}

// scopeAsk is one scope question and what it asks: the names its argument resolves to, or why it resolves to none.
type scopeAsk struct {
	question   scopeQuestion
	names      scopeNames
	unresolved error
}

// parameterPlace is where a parameter lies: the function this package declares with it, its index among the
// function's parameters, and whether it is the variadic last one.
type parameterPlace struct {
	function *ast.FuncDecl
	index    int
	variadic bool
}

// scopeNameResolver reads what a scope question of loaded, a package checked from source, asks: every parameter of a
// function it declares, every call of each function it declares, and the functions it uses other than by calling them.
type scopeNameResolver struct {
	loaded     *packages.Package
	parameters map[*types.Var]parameterPlace
	calls      map[*types.Func][]*ast.CallExpr
	valueUses  map[*types.Func]bool
}

// newScopeNameResolver indexes loaded's parameters and calls for scopeNameResolver.
func newScopeNameResolver(loaded *packages.Package) *scopeNameResolver {
	resolver := &scopeNameResolver{loaded: loaded, parameters: map[*types.Var]parameterPlace{},
		calls: map[*types.Func][]*ast.CallExpr{}, valueUses: map[*types.Func]bool{}}
	called := map[*ast.Ident]*ast.CallExpr{}
	for _, file := range loaded.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				index := 0
				for fieldIndex, field := range node.Type.Params.List {
					_, isVariadic := field.Type.(*ast.Ellipsis)
					for _, name := range field.Names {
						if parameter, isParameter := loaded.TypesInfo.Defs[name].(*types.Var); isParameter {
							resolver.parameters[parameter] = parameterPlace{function: node, index: index,
								variadic: isVariadic && fieldIndex == len(node.Type.Params.List)-1}
						}
						index++
					}
				}
			case *ast.CallExpr:
				callee := ast.Unparen(node.Fun)
				if instance, isInstance := callee.(*ast.IndexExpr); isInstance {
					callee = instance.X
				}
				if instance, isInstance := callee.(*ast.IndexListExpr); isInstance {
					callee = instance.X
				}
				if selector, isSelector := callee.(*ast.SelectorExpr); isSelector {
					callee = selector.Sel
				}
				if identifier, isIdentifier := callee.(*ast.Ident); isIdentifier {
					called[identifier] = node
				}
			}
			return true
		})
	}
	for identifier, used := range loaded.TypesInfo.Uses {
		function, isFunction := used.(*types.Func)
		if !isFunction || function.Pkg() != loaded.Types {
			continue
		}
		if call, isCalled := called[identifier]; isCalled {
			resolver.calls[function.Origin()] = append(resolver.calls[function.Origin()], call)
			continue
		}
		resolver.valueUses[function.Origin()] = true
	}
	return resolver
}

// resolve is what written, a scope question's name, asks, in one of the five shapes the guard reads: a constant
// string; a parameter of a function this package declares, every call of which passes an argument that resolves;
// `X.Name()` of a *types.PkgName, an import's own name; a constant string, or a parameter given one, followed by
// strconv.Itoa of an integer or a `%d` format of one, which numbers it; or `X.Name` of an *ast.Ident, the name of an
// identifier the body uses. visiting are the parameters being resolved, so a function that passes its own parameter
// on to itself is refused rather than followed for ever. Any other expression is refused, with its shape.
func (resolver *scopeNameResolver) resolve(written ast.Expr, visiting map[*types.Var]bool) (scopeNames, error) {
	expression := ast.Unparen(written)
	information := resolver.loaded.TypesInfo
	if value := information.Types[expression].Value; value != nil {
		if value.Kind() != constant.String {
			return scopeNames{}, fmt.Errorf("the constant %s, which is no string", value)
		}
		return scopeNames{names: []string{constant.StringVal(value)}}, nil
	}
	switch typed := expression.(type) {
	case *ast.Ident:
		if variable, isVariable := information.Uses[typed].(*types.Var); isVariable {
			return resolver.resolveParameter(variable, visiting)
		}
	case *ast.CallExpr:
		if resolver.isImportName(typed) {
			return scopeNames{importName: true}, nil
		}
	case *ast.BinaryExpr:
		if typed.Op == token.ADD && resolver.isDecimalInteger(typed.Y) {
			prefix, prefixError := resolver.resolve(typed.X, visiting)
			if prefixError != nil {
				return scopeNames{}, fmt.Errorf("%s followed by a decimal integer, whose prefix is %w",
					types.ExprString(typed.X), prefixError)
			}
			if len(prefix.prefixes) > 0 || prefix.uses || prefix.importName {
				return scopeNames{}, fmt.Errorf("%s followed by a decimal integer, whose prefix is no constant "+
					"string nor a parameter given only those", types.ExprString(typed.X))
			}
			return scopeNames{prefixes: prefix.names}, nil
		}
	case *ast.SelectorExpr:
		if resolver.isIdentifierName(typed) {
			return scopeNames{uses: true}, nil
		}
	}
	return scopeNames{}, fmt.Errorf("%s, %s", types.ExprString(expression), expressionShape(expression))
}

// expressionShape names the shape of expression, a name the guard cannot read, for its failure.
func expressionShape(expression ast.Expr) string {
	switch expression.(type) {
	case *ast.Ident:
		return "which is no variable"
	case *ast.CallExpr:
		return "a call that is no import's Name()"
	case *ast.BinaryExpr:
		return "an operation that is no constant followed by strconv.Itoa or a %d format of an integer"
	case *ast.SelectorExpr:
		return "a selector that is no *ast.Ident's Name"
	case *ast.IndexExpr:
		return "an index into a value the guard cannot read"
	}
	return fmt.Sprintf("an expression of the shape %T", expression)
}

// resolveParameter is what variable asks, a parameter given as a scope question's name: what every call of its
// function passes for it, each resolved in turn. A variable that is no parameter of a function this package declares,
// a variadic one, a method's, whose calls through an interface name another method, an exported function's, which
// another package can call, one its function assigns, and one of a function used other than by a call or never
// called, are refused, since what they hold cannot be read from the calls alone.
func (resolver *scopeNameResolver) resolveParameter(variable *types.Var, visiting map[*types.Var]bool) (
	scopeNames, error,
) {
	place, isParameter := resolver.parameters[variable]
	if !isParameter {
		return scopeNames{}, fmt.Errorf("the variable %s, which is no parameter of a function this package declares, "+
			"so what it holds is not read", variable.Name())
	}
	declaration := place.function
	switch {
	case visiting[variable]:
		return scopeNames{}, fmt.Errorf("the parameter %s of %s, which a call of %s passes on to itself",
			variable.Name(), declaration.Name.Name, declaration.Name.Name)
	case place.variadic:
		return scopeNames{}, fmt.Errorf("the variadic parameter %s of %s", variable.Name(), declaration.Name.Name)
	case declaration.Recv != nil:
		return scopeNames{}, fmt.Errorf("the parameter %s of the method %s, whose calls through an interface are not "+
			"read", variable.Name(), declaration.Name.Name)
	case ast.IsExported(declaration.Name.Name):
		return scopeNames{}, fmt.Errorf("the parameter %s of the exported %s, which another package's calls can pass",
			variable.Name(), declaration.Name.Name)
	case assigns(resolver.loaded.TypesInfo, declaration, variable):
		return scopeNames{}, fmt.Errorf("the parameter %s of %s, which %s assigns", variable.Name(),
			declaration.Name.Name, declaration.Name.Name)
	}
	function, isFunction := resolver.loaded.TypesInfo.Defs[declaration.Name].(*types.Func)
	if !isFunction {
		return scopeNames{}, fmt.Errorf("the parameter %s of %s, which declares no function", variable.Name(),
			declaration.Name.Name)
	}
	calls := resolver.calls[function.Origin()]
	switch {
	case resolver.valueUses[function.Origin()]:
		return scopeNames{}, fmt.Errorf("the parameter %s of %s, which is used other than by a call, so not every "+
			"call of it is read", variable.Name(), declaration.Name.Name)
	case len(calls) == 0:
		return scopeNames{}, fmt.Errorf("the parameter %s of %s, which nothing calls", variable.Name(),
			declaration.Name.Name)
	}
	inner := maps.Clone(visiting)
	inner[variable] = true
	var resolved scopeNames
	for _, call := range calls {
		position := resolver.loaded.Fset.Position(call.Pos())
		if place.index >= len(call.Args) {
			return scopeNames{}, fmt.Errorf("the parameter %s of %s, which the call at %s:%d passes no argument of "+
				"its own", variable.Name(), declaration.Name.Name, filepath.Base(position.Filename), position.Line)
		}
		names, argumentError := resolver.resolve(call.Args[place.index], inner)
		if argumentError != nil {
			return scopeNames{}, fmt.Errorf("the parameter %s of %s, which the call at %s:%d gives %w",
				variable.Name(), declaration.Name.Name, filepath.Base(position.Filename), position.Line,
				argumentError)
		}
		resolved = mergedScopeNames(resolved, names)
	}
	return resolved, nil
}

// assigns reports whether declaration's body assigns variable, its parameter, or takes its address, so what it holds
// at a question is not what its calls passed.
func assigns(information *types.Info, declaration *ast.FuncDecl, variable *types.Var) bool {
	isVariable := func(expression ast.Expr) bool {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		return isIdentifier && information.Uses[identifier] == variable
	}
	assigned := false
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			assigned = assigned || slices.ContainsFunc(node.Lhs, isVariable)
		case *ast.IncDecStmt:
			assigned = assigned || isVariable(node.X)
		case *ast.UnaryExpr:
			assigned = assigned || node.Op == token.AND && isVariable(node.X)
		case *ast.RangeStmt:
			assigned = assigned || node.Key != nil && isVariable(node.Key) ||
				node.Value != nil && isVariable(node.Value)
		}
		return !assigned
	})
	return assigned
}

// isNamedPointer reports whether typed is a pointer to the named type name of the package at packagePath.
func isNamedPointer(typed types.Type, packagePath string, name string) bool {
	pointer, isPointer := types.Unalias(typed).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	return isNamed && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath &&
		named.Obj().Name() == name
}

// isImportName reports whether call is `X.Name()` where X is a *types.PkgName, an import's own name: go/types'
// (*PkgName).Name, which object declares and PkgName promotes.
func (resolver *scopeNameResolver) isImportName(call *ast.CallExpr) bool {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector || len(call.Args) != 0 || selector.Sel.Name != "Name" {
		return false
	}
	selection, isSelection := resolver.loaded.TypesInfo.Selections[selector]
	return isSelection && selection.Kind() == types.MethodVal && selection.Obj().Pkg() != nil &&
		selection.Obj().Pkg().Path() == "go/types" &&
		isNamedPointer(resolver.loaded.TypesInfo.TypeOf(selector.X), "go/types", "PkgName")
}

// isIdentifierName reports whether selector is `X.Name` where X is an *ast.Ident: go/ast's Ident.Name field, the name
// of an identifier the body uses.
func (resolver *scopeNameResolver) isIdentifierName(selector *ast.SelectorExpr) bool {
	selection, isSelection := resolver.loaded.TypesInfo.Selections[selector]
	return isSelection && selection.Kind() == types.FieldVal && selector.Sel.Name == "Name" &&
		isNamedPointer(resolver.loaded.TypesInfo.TypeOf(selector.X), "go/ast", "Ident")
}

// isDecimalInteger reports whether expression spells an integer in decimal digits: strconv.Itoa of one, or fmt.Sprintf
// with the format `%d` alone and one integer. A negative integer's sign is no digit, and the guard does not read the
// sign: every numbering the rules' code asks counts up from 0.
func (resolver *scopeNameResolver) isDecimalInteger(expression ast.Expr) bool {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall {
		return false
	}
	callee := typeutil.StaticCallee(resolver.loaded.TypesInfo, call)
	if callee == nil || callee.Pkg() == nil {
		return false
	}
	switch {
	case callee.Pkg().Path() == "strconv" && callee.Name() == "Itoa":
		return len(call.Args) == 1
	case callee.Pkg().Path() == "fmt" && callee.Name() == "Sprintf":
		if len(call.Args) != 2 {
			return false
		}
		format := resolver.loaded.TypesInfo.Types[call.Args[0]].Value
		integer, isBasic := resolver.loaded.TypesInfo.TypeOf(call.Args[1]).Underlying().(*types.Basic)
		return format != nil && format.Kind() == constant.String && constant.StringVal(format) == "%d" && isBasic &&
			integer.Info()&types.IsInteger != 0
	}
	return false
}

// scopeAsksOf are what each of questions asks, read from typed, the packages holding them checked from source, by
// their import paths: the call at each question's position, its name resolved (scopeNameResolver). A question the
// load does not hold, in a file its build does not compile, is refused, since its name cannot be read.
func scopeAsksOf(questions []scopeQuestion, typed map[string]*packages.Package) []scopeAsk {
	resolvers := map[string]*scopeNameResolver{}
	calls := map[token.Position]*ast.CallExpr{}
	for packagePath, loaded := range typed {
		resolvers[packagePath] = newScopeNameResolver(loaded)
		for _, file := range loaded.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				if call, isCall := node.(*ast.CallExpr); isCall && asksScope(call) != "" {
					calls[loaded.Fset.Position(call.Pos())] = call
				}
				return true
			})
		}
	}
	asks := make([]scopeAsk, 0, len(questions))
	for _, question := range questions {
		ask := scopeAsk{question: question}
		call, isCall := calls[question.position]
		resolver := resolvers[question.packagePath]
		switch {
		case !isCall || resolver == nil:
			ask.unresolved = fmt.Errorf("a question in a file the guard's load does not compile, whose name it " +
				"cannot read")
		case len(call.Args) == 0:
			ask.unresolved = fmt.Errorf("a question with no name")
		default:
			ask.names, ask.unresolved = resolver.resolve(call.Args[0], map[*types.Var]bool{})
		}
		asks = append(asks, ask)
	}
	return asks
}

// scopeQuestionFailures are the failures of asks against declared, rule_inputs.txt's Scope= (Declared.Scope). Each
// question is filed under the rules byPackage holds for the package its source lies in (rulesByPackage). Under each, a
// rule that declares scopePackage, a Composes rule asking of its whole package, asks every name; any other must
// declare each name the question asks (Declared.AsksScopeName), the prefix followed by scopeDigits for each prefix it
// numbers, and scopeUses for an identifier's name; and a question whose name resolves to no shape the guard reads
// fails, saying which. A rule that declares scopeUses must ask an identifier's name somewhere, the one name the body
// reach honours that entry for, by Shadowed. A question in a package that holds no rule's code fails where reaching
// names a rule package whose code reaches it, a rule's helper, whose rules cannot be told, and is otherwise the
// engine's own machinery under the rules' directory, which no rule's finding reads, named in returned's second part,
// for the log.
func scopeQuestionFailures(
	asks []scopeAsk,
	byPackage map[string][]string,
	reaching map[string][]string,
	declared map[string]Declared,
) ([]string, []string) {
	var failures []string
	var machinery []string
	asksUses := map[string]bool{}
	fix := fmt.Sprintf("name each name it asks in Scope=, a prefix followed by %s for one it numbers, %s for an "+
		"identifier's name its body uses, or %s for a Composes rule's whole package", scopeDigits, scopeUses,
		scopePackage)
	for _, ask := range asks {
		place := ask.question.place
		ruleNames := byPackage[ask.question.packagePath]
		if len(ruleNames) == 0 {
			if reachers := reaching[ask.question.packagePath]; len(reachers) > 0 {
				failures = append(failures, fmt.Sprintf("%s asks a scope in a rule's helper, which the code of %s "+
					"reaches, whose rules cannot be told; move the question into the rule's own package", place,
					strings.Join(reachers, ", ")))
				continue
			}
			machinery = append(machinery, place)
			continue
		}
		for _, ruleName := range ruleNames {
			declaration := declared[ruleName]
			entries := declaration.scopeEntries()
			if slices.Contains(entries, scopePackage) {
				continue
			}
			if ask.unresolved != nil {
				failures = append(failures, fmt.Sprintf("%s asks a scope of %s by %v, a name the guard cannot "+
					"read; ask by a constant string, a parameter every call of whose function passes one, an "+
					"import's own Name(), a constant followed by strconv.Itoa of an integer, or an *ast.Ident's "+
					"Name, or declare the rule Composes with %s", place, ruleName, ask.unresolved, scopePackage))
				continue
			}
			for _, name := range ask.names.names {
				if !declaration.AsksScopeName(name) {
					failures = append(failures, fmt.Sprintf("%s asks a scope about %q, and %s does not declare it "+
						"in rule_inputs.txt: %s", place, name, ruleName, fix))
				}
			}
			for _, prefix := range ask.names.prefixes {
				if !slices.Contains(entries, prefix+scopeDigits) {
					failures = append(failures, fmt.Sprintf("%s asks a scope about %q followed by a decimal integer, "+
						"and %s does not declare %s%s in rule_inputs.txt: %s", place, prefix, ruleName, prefix,
						scopeDigits, fix))
				}
			}
			if ask.names.uses {
				asksUses[ruleName] = true
				if !slices.Contains(entries, scopeUses) {
					failures = append(failures, fmt.Sprintf("%s asks a scope about an identifier's name its body "+
						"uses, and %s does not declare %s in rule_inputs.txt: %s", place, ruleName, scopeUses, fix))
				}
			}
		}
	}
	for _, ruleName := range slices.Sorted(maps.Keys(declared)) {
		if slices.Contains(declared[ruleName].scopeEntries(), scopeUses) && !asksUses[ruleName] {
			failures = append(failures, fmt.Sprintf("%s declares %s, and no scope question of it asks an *ast.Ident's "+
				"Name, the one name the body reach's Shadowed reason honours that entry for: name what it asks",
				ruleName, scopeUses))
		}
	}
	slices.Sort(failures)
	return slices.Compact(failures), machinery
}

// loadedPackages are the packages patterns name, loaded from the rules' directory under mode, each held to load clean,
// and every package they import, the first by import path.
func loadedPackages(test *testing.T, mode packages.LoadMode, patterns ...string) (
	map[string]*packages.Package, map[string]*packages.Package,
) {
	test.Helper()
	roots, loadError := packages.Load(&packages.Config{Mode: mode}, patterns...)
	if loadError != nil {
		test.Fatal(loadError)
	}
	named, reached := map[string]*packages.Package{}, map[string]*packages.Package{}
	for _, root := range roots {
		if len(root.Errors) > 0 {
			test.Fatalf("%s does not load clean: %v", root.PkgPath, root.Errors)
		}
		named[root.PkgPath] = root
	}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		reached[visited.PkgPath] = visited
	})
	return named, reached
}

// Every scope question of the rules' code is declared, name by name (#b255cfy): each call of LookupParent or of a
// scope's Lookup in the rules' code that is no test is filed under the rules whose package's source holds it, and
// each of those rules' lines in rule_inputs.txt (Declared.Scope) names what it asks, read from the call's argument by
// the types (scopeNameResolver), so the body reach knows every name a one-side declaration could move a finding or a
// fix by. A question a package holds that no rule's code lies in fails where a rule package's code reaches it, and is
// otherwise the engine's machinery, logged: standing_facts' two, function_finder.go's objectAt and methodAt, which
// find the build's own object for one the host named, are run by the engine before any rule, and no rule package
// imports them.
//
// The guard matches spellings, so it cannot see a question asked another way: a scope held in a variable,
// `scope := pkg.Scope(); scope.Lookup(name)`; Names(), which asks of every name at once; Lookup on Parent(),
// `scope.Parent().Lookup(name)`; nor a question asked through a helper outside the rules' directory, as
// rule.Context.LookupIn and build_reads.Reads.ScopeLookup ask. The review of #b255cfy read each such site and found
// them all in Composes rules declaring (package), or asking a local scope or a foreign package.
//
// The guard runs over plants too: SA1024's package asking `len`, which no rule declares, a question in a rule's
// helper, S1018's scope dropped, and S1018 declaring (uses) for its constant `copy`, each failing as it must.
func TestEveryScopeQuestionOfARuleIsDeclared(test *testing.T) {
	test.Parallel()

	declared, parseError := DeclaredInputs()
	if parseError != nil {
		test.Fatal(parseError)
	}
	questions := scopeQuestionsIn(test, ".")
	counts := map[string]int{}
	for _, question := range questions {
		counts[question.method]++
	}
	if counts["LookupParent"] != lookupParentQuestions || counts["Lookup"] != scopeLookupQuestions {
		test.Errorf("found %d LookupParent and %d Scope().Lookup questions, want %d and %d: a question added or "+
			"gone moves the count, and a form the guard stopped seeing reads as gone", counts["LookupParent"],
			counts["Lookup"], lookupParentQuestions, scopeLookupQuestions)
	}

	_, graph := loadedPackages(test, packages.NeedName|packages.NeedImports|packages.NeedDeps, "./...")
	patterns := []string{rulesPackagePath}
	for _, question := range questions {
		if !slices.Contains(patterns, question.packagePath) {
			patterns = append(patterns, question.packagePath)
		}
	}
	typed, _ := loadedPackages(test, packages.NeedName|packages.NeedFiles|packages.NeedSyntax|packages.NeedTypes|
		packages.NeedTypesInfo|packages.NeedImports, patterns...)
	root := typed[rulesPackagePath]
	rulePackages := rulePackagesOf(root)
	wired, wiringProblems := registryWiring(root, registry)
	for _, problem := range wiringProblems {
		test.Error(problem)
	}
	byPackage := rulesByPackage(registry, rulePackages, wired)
	for _, rulePackage := range slices.Sorted(maps.Keys(rulePackages)) {
		if len(byPackage[rulePackage]) == 0 {
			test.Errorf("the registry imports %s, and the guard tells none of its rules, so its questions would "+
				"be filed under none", rulePackage)
		}
	}
	reaching := reachingRulePackages(graph, rulePackages)
	asks := scopeAsksOf(questions, typed)
	failures, machinery := scopeQuestionFailures(asks, byPackage, reaching, declared)
	for _, failure := range failures {
		test.Error(failure)
	}
	test.Logf("%d scope questions; the engine's machinery's, which no rule's code holds: %q", len(questions),
		machinery)

	trimPackage := rulesPackagePath + "/duplicate_trim_character"
	plantedUndeclared := scopeAsk{question: scopeQuestion{place: "duplicate_trim_character/analyzer.go:1",
		packagePath: trimPackage}, names: scopeNames{names: []string{"len"}}}
	helperPackage := rulesPackagePath + "/staticcheck_call"
	plantedHelper := scopeAsk{question: scopeQuestion{place: "staticcheck_call/function_check.go:1",
		packagePath: helperPackage}, names: scopeNames{names: []string{"len"}}}
	plantedFailures, _ := scopeQuestionFailures([]scopeAsk{plantedUndeclared, plantedHelper}, byPackage, reaching,
		declared)
	for _, want := range []string{
		"duplicate_trim_character/analyzer.go:1 asks a scope about \"len\", and cohere-go/SA1024 does not declare it",
		"staticcheck_call/function_check.go:1 asks a scope in a rule's helper",
	} {
		if !slices.ContainsFunc(plantedFailures, func(failure string) bool {
			return strings.HasPrefix(failure, want)
		}) {
			test.Errorf("over the planted questions, want a failure starting %q, got %q", want, plantedFailures)
		}
	}

	for _, plant := range []struct {
		scope string
		want  []string
	}{
		{scope: "", want: []string{"asks a scope about \"copy\", and cohere-go/S1018 does not declare it"}},
		{scope: scopeUses, want: []string{
			"asks a scope about \"copy\", and cohere-go/S1018 does not declare it",
			"cohere-go/S1018 declares (uses), and no scope question of it asks an *ast.Ident's Name",
		}},
	} {
		planted := maps.Clone(declared)
		dropped := planted[rule.Namespace+"S1018"]
		dropped.Scope = plant.scope
		planted[rule.Namespace+"S1018"] = dropped
		plantedFailures, _ := scopeQuestionFailures(asks, byPackage, reaching, planted)
		for _, want := range plant.want {
			if !slices.ContainsFunc(plantedFailures, func(failure string) bool {
				return strings.Contains(failure, want)
			}) {
				test.Errorf("with S1018's scope %q, want a failure saying %q, got %q", plant.scope, want,
					plantedFailures)
			}
		}
	}
}
