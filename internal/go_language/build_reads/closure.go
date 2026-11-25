package build_reads

import (
	"go/types"
	"iter"
)

// ClosurePackages are roots and every package they reach through their imports, each once, in the order a walk
// first meets them, the walk stopping where its caller stops ranging. A rule's scan of the import closure walks it
// from the judged package's imports, and the restore walks the same from that package's imports in another build, so
// both ask the same packages (#b2t9xmf).
func ClosurePackages(roots []*types.Package) iter.Seq[*types.Package] {
	return func(yield func(*types.Package) bool) {
		visited := make(map[*types.Package]bool, len(roots))
		pending := make([]*types.Package, 0, len(roots))
		for index := len(roots) - 1; index >= 0; index-- {
			pending = append(pending, roots[index])
		}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if visited[current] {
				continue
			}
			visited[current] = true
			if !yield(current) {
				return
			}
			imports := current.Imports()
			for index := len(imports) - 1; index >= 0; index-- {
				if !visited[imports[index]] {
					pending = append(pending, imports[index])
				}
			}
		}
	}
}

// ClosureInterface is the interface the package at path declares as name, and the type name that declares it, when
// that package is among roots and what they reach, both nil when it is not, or when what it declares so is no
// interface type. It is the question a Closure lookup asks (QueryClosure), as a rule finds io.Reader without importing
// io, so the rule and the restore asking it again in another build ask it alike. A closure holds one package of a
// path, so the answer does not hang on the order of the walk.
func ClosureInterface(roots []*types.Package, path string, name string) (*types.Interface, *types.TypeName) {
	for reached := range ClosurePackages(roots) {
		if reached.Path() != path {
			continue
		}
		typeName, isTypeName := reached.Scope().Lookup(name).(*types.TypeName)
		if !isTypeName {
			return nil, nil
		}
		declared, isInterface := typeName.Type().Underlying().(*types.Interface)
		if !isInterface {
			return nil, nil
		}
		return declared, typeName
	}
	return nil, nil
}
