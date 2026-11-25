package load

import (
	"go/ast"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"
)

// Bodiless is, by package, the objectpaths of the functions and methods each package checked from source declares
// without a body, as a compiler intrinsic or an assembly function is, and of their parameters and results (#b2t9xmf).
// Export data cannot say whether a function has a body, and a fact kind computed on demand answers a body-less one
// from its types alone, so a run that restores the package from its entry answers such a question without loading it
// again, where it could not tell the function from one with a body it never loaded. The load records it while it
// holds the package's syntax, since a dependency's goes before the findings cache encodes its entry. A run beside the
// load reads it as the load fills it, so it is read and written under a lock.
type Bodiless struct {
	lock     sync.Mutex
	recorded map[*packages.Package][]objectpath.Path
}

// newBodiless is an empty record, which the load fills as it checks each package.
func newBodiless() *Bodiless {
	return &Bodiless{recorded: map[*packages.Package][]objectpath.Path{}}
}

// Of is the objectpaths of what checked declares without a body, its functions' and methods' and their parameters'
// and results', and whether they were recorded: not for a package the load read from export data, nor one checked
// for its types alone, whose syntax names no object, nor on a nil record, as a caller that never loaded has.
func (bodiless *Bodiless) Of(checked *packages.Package) ([]objectpath.Path, bool) {
	if bodiless == nil {
		return nil, false
	}
	bodiless.lock.Lock()
	defer bodiless.lock.Unlock()
	paths, recorded := bodiless.recorded[checked]
	return paths, recorded
}

// record keeps what checked, just type-checked from syntax, declares without a body. A package checked with no type
// maps has no Defs to name its declarations' objects, so it records nothing, and a run restoring it loads it again to
// answer what its entry does not, as it would without the record. A declaration the load later strips of its body, as
// withholdCgoDeclarations does, is recorded as one with a body, which only ever costs that load again. An object with
// no objectpath is left out: no run can ask of it by path.
func (bodiless *Bodiless) record(checked *packages.Package) {
	if checked.TypesInfo == nil || checked.TypesInfo.Defs == nil {
		return
	}
	var encoder objectpath.Encoder
	paths := []objectpath.Path{}
	add := func(object types.Object) {
		if path, pathError := encoder.For(object); pathError == nil {
			paths = append(paths, path)
		}
	}
	for _, file := range checked.Syntax {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body != nil {
				continue
			}
			declared, isDeclared := checked.TypesInfo.Defs[function.Name].(*types.Func)
			if !isDeclared {
				continue
			}
			add(declared)
			signature := declared.Signature()
			for parameter := range signature.Params().Variables() {
				add(parameter)
			}
			for result := range signature.Results().Variables() {
				add(result)
			}
		}
	}
	bodiless.lock.Lock()
	defer bodiless.lock.Unlock()
	bodiless.recorded[checked] = paths
}
