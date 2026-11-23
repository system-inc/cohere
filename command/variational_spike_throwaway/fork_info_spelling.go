package main

import (
	"fmt"
	"go/token"

	"github.com/system-inc/go-types/canonical"
	"github.com/system-inc/go-types/types"
)

// forkInfoSpelling spells the fork's Info answers for one build as plainInfoSpelling spells the oracle's, so the two
// compare as strings.
type forkInfoSpelling struct {
	fileSet *token.FileSet
	places  declarationPlaces
	build   types.Mask
}

// object spells object by its kind, package, name, type in the build and where it is declared, or as nil for none (a
// package clause's name).
func (spelling forkInfoSpelling) object(object types.Object) string {
	if object == nil {
		return spelledNil
	}
	packagePath := ""
	if object.Pkg() != nil {
		packagePath = object.Pkg().Path()
	}
	return spelledObject(fmt.Sprintf("%T", object), packagePath, object.Name(),
		canonical.Spell(spelling.typeInBuild(object)), spelling.places.of(packagePath,
			spelling.fileSet.Position(object.Pos())))
}

// typeInBuild is object's type in the build: its answer for the build when it has answers by build.
func (spelling forkInfoSpelling) typeInBuild(object types.Object) types.Type {
	for _, variant := range types.VariantsOf(object) {
		if variant.Builds&spelling.build != 0 {
			return variant.Type
		}
	}
	return object.Type()
}

// selection spells selection by its kind, index, indirection and object.
func (spelling forkInfoSpelling) selection(selection *types.Selection) string {
	return spelledSelection(int(selection.Kind()), selection.Index(), selection.Indirect(),
		spelling.object(selection.Obj()))
}

// instance spells instance by its type arguments and type.
func (spelling forkInfoSpelling) instance(instance types.Instance) string {
	arguments := make([]string, 0, instance.TypeArgs.Len())
	for index := range instance.TypeArgs.Len() {
		arguments = append(arguments, canonical.Spell(instance.TypeArgs.At(index)))
	}
	return spelledInstance(arguments, canonical.Spell(instance.Type))
}
