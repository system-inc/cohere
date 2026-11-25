package intermediate_representation

// This file creates the members of a package created from its types alone, each on its first lookup (#npb86tz).

import (
	"go/types"
)

// memberOf is the member for object, a package-level constant, variable or function of irPackage or a method of
// one of its named types, or nil where object is none of these: a type name, a field, a local, an instance's
// method. A package created from its types alone creates the member on its first lookup, once, however many
// builds look it up at the same time (#npb86tz).
func (irPackage *Package) memberOf(object types.Object) Member {
	if !irPackage.fromTypes {
		return irPackage.values[object]
	}
	irPackage.membersMutex.Lock()
	defer irPackage.membersMutex.Unlock()
	if member, created := irPackage.values[object]; created {
		return member
	}
	if !declaresValue(irPackage.Types, object) {
		return nil
	}
	memberFromObject(irPackage, object, nil, "")
	return irPackage.values[object]
}

// memberNamed is irPackage's member named name, or nil where it has none. A package created from its types alone
// creates the member of its scope's object of that name on its first lookup, once.
func (irPackage *Package) memberNamed(name string) Member {
	if !irPackage.fromTypes {
		return irPackage.members[name]
	}
	irPackage.membersMutex.Lock()
	defer irPackage.membersMutex.Unlock()
	if member, created := irPackage.members[name]; created {
		return member
	}
	if object := irPackage.Types.Scope().Lookup(name); object != nil {
		irPackage.createMemberOnce(object)
	}
	return irPackage.members[name]
}

// createEveryMember creates each member a package created from its types alone hasn't yet, as go/ir's CreatePackage
// creates them all, so its members hold go/ir's and its Functions every function.
func (irPackage *Package) createEveryMember() {
	irPackage.membersMutex.Lock()
	defer irPackage.membersMutex.Unlock()
	if irPackage.everyMember {
		return
	}
	forEachScopeMember(irPackage.Types, irPackage.createMemberOnce)
	irPackage.everyMember = true
}

// createMemberOnce creates the member for object, an object forEachScopeMember names, unless it was created: a type
// name's is found by its name, as only members holds it, and any other by its object. The caller holds
// membersMutex.
func (irPackage *Package) createMemberOnce(object types.Object) {
	if _, isTypeName := object.(*types.TypeName); isTypeName {
		if _, created := irPackage.members[object.Name()]; created {
			return
		}
	} else if _, created := irPackage.values[object]; created {
		return
	}
	memberFromObject(irPackage, object, nil, "")
}

// declaresValue is whether object is one packageTypes, created from its types, has a value member for: a constant,
// variable or function its scope holds, or a method of a named type its scope holds, one forEachScopeMember names.
func declaresValue(packageTypes *types.Package, object types.Object) bool {
	scope := packageTypes.Scope()
	switch object := object.(type) {
	case *types.Const, *types.Var:
		return scope.Lookup(object.Name()) == object
	case *types.Func:
		receiver := object.Signature().Recv()
		if receiver == nil {
			return scope.Lookup(object.Name()) == object
		}
		receiverType := types.Unalias(receiver.Type())
		if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
			receiverType = types.Unalias(pointer.Elem())
		}
		named, isNamed := receiverType.(*types.Named)
		if !isNamed {
			return false
		}
		origin := named.Origin()
		if scope.Lookup(origin.Obj().Name()) != origin.Obj() {
			return false
		}
		for index, methodCount := 0, origin.NumMethods(); index < methodCount; index++ {
			if origin.Method(index) == object {
				return true
			}
		}
		return false
	default:
		return false
	}
}
