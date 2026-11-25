package string_int_conv

import (
	"go/types"
	"strings"
)

// describe is how a finding names described, a type within the type set of within, whose name is withinName where
// that is not empty: within itself where the two are one type, or described's own name, followed in parentheses by its
// underlying type's name where that differs, and by `in` and withinName where described is a member of another type's
// set. withinName is passed in, not read from within, so an alias names a type as it was written, a name within itself
// cannot reach. It is empty for a type with no name. x/tools' stringintconv describe.
func describe(described types.Type, within types.Type, withinName string) string {
	name := withinName
	if described != within {
		name = typeName(described)
	}
	if name == "" {
		return ""
	}
	var parentheticals []string
	if underlyingName := typeName(described.Underlying()); underlyingName != "" && underlyingName != name {
		parentheticals = append(parentheticals, underlyingName)
	}
	if described != within && withinName != "" && withinName != name {
		parentheticals = append(parentheticals, "in "+withinName)
	}
	if len(parentheticals) == 0 {
		return name
	}
	return name + " (" + strings.Join(parentheticals, ", ") + ")"
}

// typeName is named's name: a basic type's, which may be one with no type name, such as `untyped int`, or the name of
// the declared type, alias or type parameter it is, and empty for a type of any other kind. x/tools' stringintconv
// typeName.
func typeName(named types.Type) string {
	if basic, isBasic := named.(*types.Basic); isBasic {
		return basic.Name()
	}
	if declared := typeNameFor(named); declared != nil {
		return declared.Name()
	}
	return ""
}

// typeNameFor is the type name that declares named, an alias, a declared type or a type parameter, or the universe's
// name for a basic type, and nil for a type of any other kind. x/tools' typesinternal.TypeNameFor.
func typeNameFor(named types.Type) *types.TypeName {
	switch declared := named.(type) {
	case *types.Alias:
		return declared.Obj()
	case *types.Named:
		return declared.Obj()
	case *types.TypeParam:
		return declared.Obj()
	case *types.Basic:
		// Kept for fidelity, and no input reaches it: typeName, its one caller, answers a basic type itself first.
		if universal, isTypeName := types.Universe.Lookup(declared.Name()).(*types.TypeName); isTypeName {
			return universal
		}
	}
	return nil
}
