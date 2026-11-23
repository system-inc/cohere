package main

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// plainInfoSpelling spells the oracle's Info answers, go/types' own, as forkInfoSpelling spells the projection's.
type plainInfoSpelling struct {
	fileSet *token.FileSet
	places  declarationPlaces
}

// object spells object by its kind, package, name, type and where it is declared, or as nil for none.
func (spelling plainInfoSpelling) object(object types.Object) string {
	if object == nil {
		return spelledNil
	}
	packagePath := ""
	if object.Pkg() != nil {
		packagePath = object.Pkg().Path()
	}
	return spelledObject(fmt.Sprintf("%T", object), packagePath, object.Name(), spelledPlainType(object.Type()),
		spelling.places.of(packagePath, spelling.fileSet.Position(object.Pos())))
}

// selection spells selection by its kind, index, indirection and object.
func (spelling plainInfoSpelling) selection(selection *types.Selection) string {
	return spelledSelection(int(selection.Kind()), selection.Index(), selection.Indirect(),
		spelling.object(selection.Obj()))
}

// instance spells instance by its type arguments and type.
func (spelling plainInfoSpelling) instance(instance types.Instance) string {
	arguments := make([]string, 0, instance.TypeArgs.Len())
	for index := range instance.TypeArgs.Len() {
		arguments = append(arguments, spelledPlainType(instance.TypeArgs.At(index)))
	}
	return spelledInstance(arguments, spelledPlainType(instance.Type))
}

// spelledPlainType is typed as the census spells it, which canonical.Spell spells the fork's types alike.
func spelledPlainType(typed types.Type) string {
	var builder strings.Builder
	writeCanonicalType(&builder, typed)
	return builder.String()
}
