package main

import "fmt"

// misnamedFile is a file the file name rule flags: it serves exactly one type and its name does not name it.
type misnamedFile struct {
	file     *sourceFile
	typeName string
	// declared is whether the file declares the type, rather than holding only methods of it.
	declared bool
	// suggestion is the name the rule's finding suggests.
	suggestion string
	// typeLines is the lines the file gives the type, its declaration and methods with their docs, and
	// declarationLines the lines of every declaration of the file but its imports, with their docs.
	typeLines        int
	declarationLines int
	// part is what the file holds of the type.
	part *part
	// nameInCode is whether Go code spells the file's name in a string literal, so it must keep it.
	nameInCode bool
}

// isMinority is whether the type takes less than half the lines of the file's declarations. Such a file is about
// something else, so the type moves out to a file named for it and the file keeps its name; a file mostly about its
// type takes the type's name.
func (misnamed misnamedFile) isMinority() bool {
	return misnamed.typeLines*2 < misnamed.declarationLines
}

// extractable is whether the split moves the type out rather than the name pass renaming the file: the type is a
// minority of it, or the file is already named for something else it declares, and the file and the type may move.
func (misnamed misnamedFile) extractable() bool {
	return (misnamed.isMinority() || misnamed.namesOtherDeclaration() || misnamed.nameInCode) &&
		misnamed.file.pinned == "" && !misnamed.part.ambiguous
}

// namesOtherDeclaration is whether the file's name spells its package or another of its top-level declarations,
// as served.go is package served's and analyzer.go holds Analyzer: the file is named for that, and renaming it for
// the type would take the name from what a reader opens it for.
func (misnamed misnamedFile) namesOtherDeclaration() bool {
	stem := fileStem(misnamed.file.name)
	allowedTail := isToolchainSuffix
	if hasBuildConstraint(misnamed.file.syntax) {
		allowedTail = isVariantSuffix
	}
	for _, name := range append(topLevelNames(misnamed.file.syntax), misnamed.file.syntax.Name.Name) {
		if name != misnamed.typeName && namesType(stem, name, allowedTail) {
			return true
		}
	}
	return false
}

// kind says what the file holds of its type, and how much of the file that is, for the report.
func (misnamed misnamedFile) kind() string {
	share := fmt.Sprintf(", %d of %d lines", misnamed.typeLines, misnamed.declarationLines)
	if misnamed.declared {
		return "declares " + misnamed.typeName + share
	}
	return "methods of " + misnamed.typeName + share
}
