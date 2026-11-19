package odd_slice_argument

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// marks is SA5012's state in one package: the marks of each function judged, by the function, every one of them the
// definition's and so final (see demandWalk), and where each parameter asked of is declared, nil for one no function
// of the package declares (see ownerOf).
type marks struct {
	functions map[*intermediate_representation.Function]map[types.Object]bool
	owners    map[*types.Var]*parameterOwner
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
		state.owners = map[*types.Var]*parameterOwner{}
	}
	owner, found := state.owners[parameter]
	if !found {
		owner = ownerOf(home, parameter)
		state.owners[parameter] = owner
	}
	if owner == nil {
		return nil
	}
	var enclosing *intermediate_representation.Function
	if owner.declared != nil {
		// Built by its package's Function, as any function a question reaches is.
		enclosing = home.Function(owner.declared)
	} else if enclosing = home.IntermediateRepresentation().Function("init"); enclosing != nil {
		resolver.Build(enclosing)
	}
	if enclosing == nil {
		return nil
	}
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

// ownerOf is where parameter is declared, as a named parameter of one of home's declared functions or of a function
// literal in home, read from the syntax at parameter's position, or nil when home declares it so nowhere: a result, a
// receiver, an interface method's parameter, another package's parameter, or an instance's, which no name of home
// defines. Only the declaration holding the position is read, and only down to it. A receiver is left out, since no
// call passes a slice to one as a parameter.
func ownerOf(home *fact_resolver.Package, parameter *types.Var) *parameterOwner {
	for _, file := range home.Files() {
		if parameter.Pos() < file.FileStart || parameter.Pos() > file.FileEnd {
			continue
		}
		for _, declaration := range file.Decls {
			if parameter.Pos() < declaration.Pos() || parameter.Pos() >= declaration.End() {
				continue
			}
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				// A function literal at package level runs in the package's initializer.
				return literalOwner(home, parameter, declaration, nil)
			}
			declared, isDeclared := home.Info().Defs[function.Name].(*types.Func)
			if !isDeclared {
				return nil
			}
			if declares(home, function.Type.Params, parameter) {
				return &parameterOwner{declared: declared}
			}
			if function.Body == nil {
				return nil
			}
			return literalOwner(home, parameter, function.Body, declared)
		}
		return nil
	}
	return nil
}

// literalOwner is the function literal inside node whose own parameters declare parameter, the innermost one holding
// parameter's position, inside declared, nil for a literal at package level, or nil when none declares it.
func literalOwner(
	home *fact_resolver.Package,
	parameter *types.Var,
	node ast.Node,
	declared *types.Func,
) *parameterOwner {
	var owner *parameterOwner
	ast.Inspect(node, func(inspected ast.Node) bool {
		if owner != nil || inspected == nil || parameter.Pos() < inspected.Pos() || parameter.Pos() >= inspected.End() {
			return false
		}
		if literal, isLiteral := inspected.(*ast.FuncLit); isLiteral && declares(home, literal.Type.Params, parameter) {
			owner = &parameterOwner{declared: declared, literal: literal}
			return false
		}
		return true
	})
	return owner
}

// declares is whether one of the names fields declares is parameter's.
func declares(home *fact_resolver.Package, fields *ast.FieldList, parameter *types.Var) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if home.Info().Defs[name] == types.Object(parameter) {
				return true
			}
		}
	}
	return false
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
