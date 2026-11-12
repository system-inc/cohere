package consistency_no_uppercase_initialism

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// foreignInterfaces are the interfaces of every package in this package's import graph, and the predeclared error,
// by the names of their methods. A method whose receiver implements one of them is named by that interface, and a
// rename would stop it implementing it. The index is built the first time a method's name holds an initialism, since
// a package with none never needs it.
type foreignInterfaces struct {
	// current is the package whose methods are judged, where the walk through the imports starts.
	current *types.Package
	// reads is the reads of current, which each question fix answers is recorded in, nil when the run records none:
	// the answer depends on every interface of the import closure, which no type read of current holds (#b2t9xmf).
	reads *build_reads.Reads
	// rule is the name the rule was registered under, which each question names.
	rule     string
	byMethod map[string][]indexedInterface
	// genericByMethod holds the generic interfaces apart, since one is implemented only through an instantiation
	// whose type arguments the rule cannot know, and so is matched by shape rather than by types.Implements.
	genericByMethod map[string][]indexedInterface
}

// indexedInterface is one interface of the index, and the name that declares it, which a recorded answer names.
type indexedInterface struct {
	declared      *types.TypeName
	interfaceType *types.Interface
}

// fix is whether an interface of another package names method: the method's receiver type implements an interface
// declared outside this package that has a method of the same name, which implementing it makes the same signature.
// The question and its answer, the interface that fixes the name or none, are recorded in the reads.
func (interfaces *foreignInterfaces) fix(method *types.Func) bool {
	fixedBy := interfaces.fixedBy(method)
	interfaces.reads.AddLookup(build_reads.Lookup{
		Query:   build_reads.QueryInterface,
		Name:    method.Name(),
		Subject: method,
		Found:   fixedBy != nil,
		Answer:  fixedBy,
		Rule:    interfaces.rule,
	})
	return fixedBy != nil
}

// fixedBy is the name of the first interface of another package that fixes method's name (see fix), nil when none does.
func (interfaces *foreignInterfaces) fixedBy(method *types.Func) *types.TypeName {
	if interfaces.byMethod == nil {
		interfaces.build()
	}
	implementer := implementerOf(method)
	for _, candidate := range interfaces.byMethod[method.Name()] {
		if types.Implements(implementer, candidate.interfaceType) {
			return candidate.declared
		}
	}
	for _, candidate := range interfaces.genericByMethod[method.Name()] {
		if implementsInShape(implementer, candidate.interfaceType) {
			return candidate.declared
		}
	}
	return nil
}

// build indexes the interfaces of every package reachable through the imports, this package's own excepted: an
// interface declared here is ours, and its methods and their implementations are judged together.
func (interfaces *foreignInterfaces) build() {
	interfaces.byMethod = map[string][]indexedInterface{}
	interfaces.genericByMethod = map[string][]indexedInterface{}
	// The predeclared error belongs to no package, and fixes Error on every error type.
	if predeclared, isTypeName := types.Universe.Lookup("error").(*types.TypeName); isTypeName {
		interfaces.add(predeclared)
	}
	visited := map[*types.Package]bool{interfaces.current: true}
	pending := []*types.Package{interfaces.current}
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
					interfaces.add(typeName)
				}
			}
		}
	}
}

// add indexes the type declared names under each of its methods' names when it is an interface a type can implement,
// which a constraint is not: only type parameters satisfy one. A generic interface goes to its own index.
func (interfaces *foreignInterfaces) add(declared *types.TypeName) {
	interfaceType, isInterface := declared.Type().Underlying().(*types.Interface)
	if !isInterface || !interfaceType.IsMethodSet() {
		return
	}
	index := interfaces.byMethod
	if named, isNamed := types.Unalias(declared.Type()).(*types.Named); isNamed && named.TypeParams().Len() > 0 {
		index = interfaces.genericByMethod
	}
	for methodIndex := range interfaceType.NumMethods() {
		methodName := interfaceType.Method(methodIndex).Name()
		indexed := indexedInterface{declared: declared, interfaceType: interfaceType}
		index[methodName] = append(index[methodName], indexed)
	}
}
