package consistency_no_uppercase_initialism

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// foreignInterfaces are the interfaces of every package in this package's import graph, and the predeclared error,
// by the names of their methods. A method whose receiver implements one of them is named by that interface, and a
// rename would stop it implementing it. The index is built the first time a method's name holds an initialism, since
// a package with none never needs it.
type foreignInterfaces struct {
	pass     *analysis.Pass
	byMethod map[string][]*types.Interface
	// genericByMethod holds the generic interfaces apart, since one is implemented only through an instantiation
	// whose type arguments the rule cannot know, and so is matched by shape rather than by types.Implements.
	genericByMethod map[string][]*types.Interface
}

// fix is whether an interface of another package names method: the method's receiver type implements an interface
// declared outside this package that has a method of the same name, which implementing it makes the same signature.
func (interfaces *foreignInterfaces) fix(method *types.Func) bool {
	if interfaces.byMethod == nil {
		interfaces.build()
	}
	implementer := implementerOf(method)
	for _, candidate := range interfaces.byMethod[method.Name()] {
		if types.Implements(implementer, candidate) {
			return true
		}
	}
	for _, candidate := range interfaces.genericByMethod[method.Name()] {
		if implementsInShape(implementer, candidate) {
			return true
		}
	}
	return false
}

// build indexes the interfaces of every package reachable through the imports, this package's own excepted: an
// interface declared here is ours, and its methods and their implementations are judged together.
func (interfaces *foreignInterfaces) build() {
	interfaces.byMethod = map[string][]*types.Interface{}
	interfaces.genericByMethod = map[string][]*types.Interface{}
	// The predeclared error belongs to no package, and fixes Error on every error type.
	interfaces.add(types.Universe.Lookup("error").Type())
	visited := map[*types.Package]bool{interfaces.pass.Pkg: true}
	pending := []*types.Package{interfaces.pass.Pkg}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, imported := range current.Imports() {
			if visited[imported] {
				continue
			}
			visited[imported] = true
			pending = append(pending, imported)
			for _, name := range imported.Scope().Names() {
				if typeName, isTypeName := imported.Scope().Lookup(name).(*types.TypeName); isTypeName {
					interfaces.add(typeName.Type())
				}
			}
		}
	}
}

// add indexes declared under each of its methods' names when it is an interface a type can implement, which a
// constraint is not: only type parameters satisfy one. A generic interface goes to its own index.
func (interfaces *foreignInterfaces) add(declared types.Type) {
	interfaceType, isInterface := declared.Underlying().(*types.Interface)
	if !isInterface || !interfaceType.IsMethodSet() {
		return
	}
	index := interfaces.byMethod
	if named, isNamed := types.Unalias(declared).(*types.Named); isNamed && named.TypeParams().Len() > 0 {
		index = interfaces.genericByMethod
	}
	for methodIndex := range interfaceType.NumMethods() {
		methodName := interfaceType.Method(methodIndex).Name()
		index[methodName] = append(index[methodName], interfaceType)
	}
}
