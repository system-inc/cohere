package main

// What follows is the Go engine's cohere-go/consistency-no-abbreviated-identifier reading of which declared names
// are its package's own, kept to its letter (internal/goengine/rules/consistency_no_abbreviated_identifier), so
// the -global pass proposes exactly the names the rule flags.

import "go/types"

// isOurs is whether a declared object's name is this package's to choose: not an import's name, not an embedded
// field, which is named for its type, and not a method another package fixes.
func isOurs(reachable reach, object types.Object) bool {
	switch declared := object.(type) {
	case *types.PkgName:
		return false
	case *types.Var:
		return !declared.Embedded()
	case *types.Func:
		return !fixedByAnotherPackage(reachable, declared)
	}
	return true
}

// foreignMethod is a method another package declares, on an interface or on a named type.
type foreignMethod struct {
	method *types.Func
	// owner is the interface that declares the method, or nil when a concrete named type does.
	owner *types.Interface
}

// reach is what the rule knows of the packages this one reaches through its imports.
type reach struct {
	methodsByName map[string][]foreignMethod
	packageNames  map[string]bool
}

// fixedByAnotherPackage is whether a method's name is set by another package: a concrete method implementing a
// foreign interface's method of the same name and signature, or an interface method of ours that some foreign
// method of the same name and signature must be able to satisfy.
func fixedByAnotherPackage(reachable reach, method *types.Func) bool {
	receiver := method.Signature().Recv()
	if receiver == nil {
		return false
	}
	isInterfaceMethod := types.IsInterface(receiver.Type())
	named := receiverNamed(receiver.Type())
	for _, foreign := range reachable.methodsByName[method.Name()] {
		if !types.Identical(foreign.method.Signature(), method.Signature()) {
			continue
		}
		if isInterfaceMethod {
			return true
		}
		if foreign.owner == nil {
			continue
		}
		if named == nil || named.TypeParams().Len() > 0 {
			return true
		}
		if types.Implements(named, foreign.owner) || types.Implements(types.NewPointer(named), foreign.owner) {
			return true
		}
	}
	return false
}

// receiverNamed is the named type a method's receiver is, through its pointer, or nil.
func receiverNamed(receiverType types.Type) *types.Named {
	dereferenced := receiverType
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	named, isNamed := types.Unalias(dereferenced).(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// reachOf indexes every package current reaches through its imports, once each.
func reachOf(current *types.Package) reach {
	reachable := reach{methodsByName: map[string][]foreignMethod{}, packageNames: map[string]bool{}}
	visited := map[*types.Package]bool{current: true}
	pending := append([]*types.Package{}, current.Imports()...)
	for len(pending) > 0 {
		imported := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[imported] {
			continue
		}
		visited[imported] = true
		pending = append(pending, imported.Imports()...)
		reachable.packageNames[imported.Name()] = true
		for _, name := range imported.Scope().Names() {
			typeName, isTypeName := imported.Scope().Lookup(name).(*types.TypeName)
			if !isTypeName || typeName.IsAlias() {
				continue
			}
			named, isNamed := typeName.Type().(*types.Named)
			if !isNamed || named.TypeParams().Len() > 0 {
				continue
			}
			if declaredInterface, isInterface := named.Underlying().(*types.Interface); isInterface {
				for index := range declaredInterface.NumMethods() {
					method := declaredInterface.Method(index)
					reachable.methodsByName[method.Name()] = append(reachable.methodsByName[method.Name()],
						foreignMethod{method: method, owner: declaredInterface})
				}
				continue
			}
			for index := range named.NumMethods() {
				method := named.Method(index)
				reachable.methodsByName[method.Name()] = append(
					reachable.methodsByName[method.Name()],
					foreignMethod{method: method},
				)
			}
		}
	}
	return reachable
}
