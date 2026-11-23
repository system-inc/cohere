package consistency_no_any

import (
	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/rules/load_memo"
)

// foreignMethods are the exported methods of every named type one package declares, interfaces and concrete types
// alike, by name. Signature identity ignores receivers, so each is matched by its signature alone, and held as the
// method, which a recorded answer names. Nothing in them depends on the package that reaches the declaring one, so a
// load reads each package's once (see methodsByLoad).
type foreignMethods struct {
	byName map[string][]*types.Func
	// genericByName holds a generic type's methods apart, since their signatures speak of type parameters that only
	// an instantiation the rule cannot know replaces, so they are matched by shape.
	genericByName map[string][]*types.Func
}

// add indexes named's exported methods: an interface's whole method set, embedded methods included, and a concrete
// type's own declared methods. Unexported ones belong to their package, where no other package can match them.
func (foreign *foreignMethods) add(named *types.Named) {
	index := foreign.byName
	if named.TypeParams().Len() > 0 {
		index = foreign.genericByName
	}
	if interfaceType, isInterface := named.Underlying().(*types.Interface); isInterface {
		for methodIndex := range interfaceType.NumMethods() {
			method := interfaceType.Method(methodIndex)
			if method.Exported() {
				index[method.Name()] = append(index[method.Name()], method)
			}
		}
		return
	}
	for methodIndex := range named.NumMethods() {
		method := named.Method(methodIndex)
		if method.Exported() {
			index[method.Name()] = append(index[method.Name()], method)
		}
	}
}

// methodsByLoad are what the packages of each load declare, each package read once however many packages of the load
// reach it. Every package holding a method candidate once indexed its whole import closure afresh, so a package the
// tree imports everywhere was read again for each, and Scope.Names, which sorts on every call, was nearly half of
// that.
var methodsByLoad = load_memo.New(declaredMethodsOf)

// declaredMethodsOf indexes the methods of every named type declaring declares, in the order of the type's names. An
// alias adds nothing: the type it names is declared in a package of the closure, which indexes it there.
func declaredMethodsOf(declaring *types.Package) *foreignMethods {
	foreign := &foreignMethods{byName: map[string][]*types.Func{}, genericByName: map[string][]*types.Func{}}
	for _, name := range declaring.Scope().Names() {
		typeName, isTypeName := declaring.Scope().Lookup(name).(*types.TypeName)
		if !isTypeName || typeName.IsAlias() {
			continue
		}
		if named, isNamed := typeName.Type().(*types.Named); isNamed {
			foreign.add(named)
		}
	}
	return foreign
}
