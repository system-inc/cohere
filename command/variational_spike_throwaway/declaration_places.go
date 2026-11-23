package main

import (
	"go/token"
)

// declarationPlaces says where an object of an Info map is declared, as far as a build's projection and its oracle can
// agree on it. A declaration of ours in a file every build of its package compiles is at one exact position in both.
// One in a file only some builds compile is only said to be in such a file: a name declared in each build's own file is
// one merged object at its first declaration, and declarations alike across builds fold into one, so the projection's
// position for a build can be another build's file. A declaration in a package not ours, or in none (the universe's),
// says nothing of where it is: a merged object of the standard library sits at the host's declaration.
type declarationPlaces struct {
	ours map[string]*ourPackage
	// inEveryBuild says, of each file of ours by name, whether every build of its package compiles it.
	inEveryBuild map[string]bool
}

// newDeclarationPlaces reads each file of ours, parsed into fileSet, as compiled by every build of its package or not.
func newDeclarationPlaces(fileSet *token.FileSet, ours map[string]*ourPackage) declarationPlaces {
	places := declarationPlaces{ours: ours, inEveryBuild: map[string]bool{}}
	for _, our := range ours {
		for _, file := range our.files {
			places.inEveryBuild[fileSet.Position(file.Package).Filename] = our.masks[file] == our.builds
		}
	}
	return places
}

// of spells where an object of the package at packagePath, declared at position, is, as the comparison reads it.
func (places declarationPlaces) of(packagePath string, position token.Position) string {
	if _, isOurs := places.ours[packagePath]; !isOurs {
		return "not ours"
	}
	if !position.IsValid() {
		return "no position"
	}
	if places.inEveryBuild[position.Filename] {
		return position.String()
	}
	return "in a file of some builds"
}
