package odd_slice_argument

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys SA5012's marks in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "odd_slice_argument"}

// marks is SA5012's state in one package: the parameters of each function judged that it marks, by the function,
// each function's parameters by their objects, and, once the package was computed whole the push's way, every mark it
// made.
type marks struct {
	functions map[*intermediate_representation.Function]map[types.Object]bool
	owners    map[*types.Var]parameterOwner
	fellBack  bool
	recorded  map[types.Object]bool
}

// parameterOwner is where a parameter is declared: a declared function, or a function literal inside one, or inside a
// package-level declaration, whose function is the package's initializer, when declared is nil.
type parameterOwner struct {
	declared *types.Func
	literal  *ast.FuncLit
}

// ask is whether parameter is marked as a reader in reader holds it: the mark crossed here (fact_resolver.Visible), and
// parameter's function marks it. The function is judged in its own package, under that package's lock, once, and each
// answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, parameter *types.Var) bool {
	if !fact_resolver.Visible(reader, parameter) {
		return false
	}
	home := resolver.Owner(parameter)
	if home == nil {
		return false
	}
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[marks](home, Kind)
	if state.fellBack {
		return state.recorded[parameter]
	}
	owner := state.function(home, parameter)
	if owner == nil {
		// A parameter of no function the package declares, as an instantiation's is, which the push marks only in
		// the package that calls it, never here.
		return false
	}
	walk := &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state,
		partial: map[*intermediate_representation.Function]map[types.Object]bool{}}
	marked := walk.verdict(owner)
	if walk.orderDependent {
		state.fallBack(resolver, home)
		return state.recorded[parameter]
	}
	return marked[parameter]
}

// function is the function of home that declares parameter, built, or nil for a parameter no source function of home
// declares. The caller holds home's lock.
func (state *marks) function(
	home *fact_resolver.Package,
	parameter *types.Var,
) *intermediate_representation.Function {
	if state.owners == nil {
		state.owners = parameterOwners(home)
	}
	owner, declared := state.owners[parameter]
	if !declared {
		return nil
	}
	var enclosing *intermediate_representation.Function
	if owner.declared != nil {
		enclosing = home.Function(owner.declared)
	} else {
		enclosing = home.IntermediateRepresentation().Function("init")
	}
	if enclosing == nil {
		return nil
	}
	enclosing.Build()
	if owner.literal == nil {
		return enclosing
	}
	return anonymousOf(enclosing, owner.literal)
}

// anonymousOf is the anonymous function of function, or of one of its anonymous functions, built from literal.
func anonymousOf(
	function *intermediate_representation.Function,
	literal *ast.FuncLit,
) *intermediate_representation.Function {
	for _, anonymous := range function.AnonymousFunctions {
		if anonymous.Syntax() == ast.Node(literal) {
			return anonymous
		}
		if found := anonymousOf(anonymous, literal); found != nil {
			return found
		}
	}
	return nil
}

// parameterOwners is where each named parameter of home's functions and function literals is declared, read from its
// syntax. A receiver is left out, since no call passes a slice to one as a parameter.
func parameterOwners(home *fact_resolver.Package) map[*types.Var]parameterOwner {
	owners := map[*types.Var]parameterOwner{}
	add := func(fields *ast.FieldList, owner parameterOwner) {
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := home.Info().Defs[name].(*types.Var); isVariable {
					owners[variable] = owner
				}
			}
		}
	}
	addLiterals := func(node ast.Node, declared *types.Func) {
		ast.Inspect(node, func(inspected ast.Node) bool {
			if literal, isLiteral := inspected.(*ast.FuncLit); isLiteral {
				add(literal.Type.Params, parameterOwner{declared: declared, literal: literal})
			}
			return true
		})
	}
	for _, file := range home.Files() {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				addLiterals(declaration, nil)
				continue
			}
			declared, isDeclared := home.Info().Defs[function.Name].(*types.Func)
			if !isDeclared {
				continue
			}
			add(function.Type.Params, parameterOwner{declared: declared})
			if function.Body != nil {
				addLiterals(function.Body, declared)
			}
		}
	}
	return owners
}

// fallBack marks every parameter of home the push's way, every source function's own checks and then the calls in
// buildir's order with one seen set, and keeps every mark, for a package where an answer depended on that order (see
// demandWalk). A function's marks kept before are the push's by the reasoning there; one the push contradicts is
// counted, so the differential can require none.
func (state *marks) fallBack(resolver *fact_resolver.Resolver, home *fact_resolver.Package) {
	counts := resolver.Counts(Kind)
	counts.Fallbacks.Add(1)
	recorded := &packageFacts{resolver: resolver, home: home, recorded: map[types.Object]bool{}}
	walk := &pushWalk{facts: recorded}
	functions := sourceFunctions(home)
	walk.markLengthChecks(functions)
	walk.markIndirectLengthChecks(functions)
	for _, function := range functions {
		kept, judged := state.functions[function]
		if !judged {
			counts.Computed.Add(1)
			continue
		}
		for _, parameter := range function.Parameters {
			if kept[parameter.Object()] != recorded.recorded[parameter.Object()] {
				counts.Contradicted.Add(1)
			}
		}
	}
	state.fellBack = true
	state.recorded = recorded.recorded
	state.functions = nil
}

// sourceFunctions is every source function of home, built, in the order buildir's SrcFuncs lists them: the package's
// functions and methods as declared, the synthetic initializer first, then each one's anonymous functions, depth
// first, in the order they were declared in it.
func sourceFunctions(home *fact_resolver.Package) []*intermediate_representation.Function {
	declared := home.SourceFunctions()
	functions := append([]*intermediate_representation.Function{}, declared...)
	var addAnonymous func(function *intermediate_representation.Function)
	addAnonymous = func(function *intermediate_representation.Function) {
		for _, anonymous := range function.AnonymousFunctions {
			functions = append(functions, anonymous)
			addAnonymous(anonymous)
		}
	}
	for _, function := range declared {
		addAnonymous(function)
	}
	return functions
}
