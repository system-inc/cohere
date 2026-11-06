package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/ecmascript/module_roots"
)

// importerIndex is, for each file in the program, the names any other file imports from it, plus
// the files that are alive without an importer.
type importerIndex struct {
	byPath map[tspath.PathKey]*importedNames
	roots  *module_roots.Set
}

// provablyUnimported reports whether nothing in the program can import this name from this file,
// and the file is not one a framework or test runner loads without an import.
//
// Every doubt answers false, because a false here only keeps the older PascalCase advice, while a
// wrong true tells the author to drop an export something reads.
//
// Several names because one binding can leave its file under more than one: `export { x, x as Y }`.
// It is unimported only when every one of them is.
func (index *importerIndex) provablyUnimported(sourceFile *ast.SourceFile, names ...string) bool {
	if index == nil || sourceFile == nil || len(names) == 0 {
		return false
	}
	if isRoot, _ := index.roots.IsRoot(sourceFile.FileName().AsString()); isRoot {
		return false
	}
	entry := index.byPath[sourceFile.PathKey()]
	if entry == nil {
		return true
	}
	if entry.everything {
		return false
	}
	for _, name := range names {
		if entry.names[name] {
			return false
		}
	}
	return true
}
