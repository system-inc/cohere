package odd_slice_argument

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// marks is SA5012's state in one package: the marks of each function judged, by the function, every one of them the
// definition's and so final (see demandWalk), and each function's parameters by their objects.
type marks struct {
	functions map[*intermediate_representation.Function]map[types.Object]bool
	owners    map[*types.Var]parameterOwner
}

// walk is a demand walk over home's marks, with state's map made when it is the package's first.
func (state *marks) walk(resolver *fact_resolver.Resolver, home *fact_resolver.Package) *demandWalk {
	if state.functions == nil {
		state.functions = map[*intermediate_representation.Function]map[types.Object]bool{}
	}
	return &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state,
		byFunction: map[*intermediate_representation.Function]*reachedFunction{}}
}

// parameterOwner is where a parameter is declared: a declared function, or a function literal inside one, or inside a
// package-level declaration, whose function is the package's initializer, when declared is nil.
type parameterOwner struct {
	declared *types.Func
	literal  *ast.FuncLit
}

// function is the function of home that declares parameter, built, or nil for a parameter no source function of home
// declares. The caller holds home's lock.
func (state *marks) function(
	resolver *fact_resolver.Resolver,
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
	resolver.Build(enclosing)
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

// whole is every parameter of home marked, every source function judged or its marks kept, each with its answer, true,
// for the standard library's table, where a parameter absent is not marked. The order the functions are judged in
// changes no mark (see demandWalk).
func whole(resolver *fact_resolver.Resolver, home *fact_resolver.Package) map[types.Object]bool {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[marks](home, Kind)
	walk := state.walk(resolver, home)
	for _, function := range sourceFunctions(home) {
		walk.begin(function)
	}
	walk.settle()
	answers := map[types.Object]bool{}
	for _, marked := range state.functions {
		for parameter := range marked {
			answers[parameter] = true
		}
	}
	return answers
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
