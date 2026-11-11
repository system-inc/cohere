package staticcheck_code

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IndexedObject is the package-level object called name in the package of path, as staticcheck's type index of the
// context's package looks it up, and nil when the index does not hold that package or the package declares no such
// name. The index holds a package the context's package imports, a blank import among them, and the package of every
// field or method the context's package uses, never the context's own; a package reached only through another
// package's alias or variable is not held. It is the index's Object, which staticcheck's code.CouldMatchAny asks of
// each symbol a pattern names, skipping the whole package when none is held: a port of a pattern that names a Symbol
// below its root asks this before it walks anything.
func IndexedObject(context *rule.Context, path string, name string) types.Object {
	indexed := indexedPackage(context, path)
	if indexed == nil {
		return nil
	}
	// Recorded in the context's reads, as every look into another package's scope is (#b2t9xmf).
	return context.LookupIn(indexed, name)
}

// indexedPackage is the package of path the type index holds for the context's package, or nil.
func indexedPackage(context *rule.Context, path string) *types.Package {
	held := func(candidate *types.Package) bool {
		return candidate != nil && candidate != context.Package && candidate.Path() == path
	}
	for _, file := range context.Files {
		for _, specification := range file.Imports {
			if imported := context.TypesInfo.PkgNameOf(specification); imported != nil && held(imported.Imported()) {
				return imported.Imported()
			}
		}
	}
	for _, object := range context.TypesInfo.Uses {
		// The index takes a used object's package only for a field or a method: a package-level object's package is
		// one the package imports, or its own, which the index never holds.
		if isPackageLevel(object) {
			continue
		}
		if held(object.Pkg()) {
			return object.Pkg()
		}
	}
	return nil
}

// isPackageLevel is whether object is declared at its package's scope, as the index's typesinternal.IsPackageLevel
// judges it.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
