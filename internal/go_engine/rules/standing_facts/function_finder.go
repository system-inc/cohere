package standing_facts

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// functionFinder finds the build's own function, or any object, for an identity the host names one by, for one pass, on
// one goroutine. The host's objects are another run's, of another type-check, so nothing of them but the identity
// crosses, and the build looks its own object up by it.
//
// It reads only the build's own types, those of the pass's package and of what that imports, in the pass that asks,
// and keeps nothing past the pass: the finder and its map of the imports go when run returns, so no later pass, build
// or run can be answered from a look it made. None of its looks is recorded in the package's reads (#b2t9xmf): what it
// finds decides only which build lints a body, a wrong find is caught by the findings differential, and an entry's key
// holds the whole import closure the looks read (readHostNamed, in the rules' guard of cross-package reads).
type functionFinder struct {
	pass *analysis.Pass

	// imported is every package the pass's package imports, directly or through imports, by its path, with nil for a
	// path two of them share. It is nil until an identity first names another package, since most name the pass's own.
	imported map[string]*types.Package
}

// function is the build's function for identity, and false when the build has none (find), or when what it has is no
// function.
func (finder *functionFinder) function(identity build_reads.Identity) (*types.Func, bool) {
	object, found := finder.find(identity)
	function, isFunction := object.(*types.Func)
	return function, found && isFunction
}

// find is the build's object for identity, and false when the build has none: a package the pass's package does not
// import, a name or a path that reaches nothing, or an identity of a kind that names no object here, as an import's or
// an unknown one. The presence rows judge what it finds (standing_presence), each by its own declaration in the build.
//
// An object of the pass's own package must be declared in the file the identity names, since a platform's file that
// declares the same name is another declaration, with another body or value and so other facts. Another package's
// object is not held to its file: its positions come from wherever the build loaded the package from, source or export
// data, which are not the host's to compare, and where its declaring file differs by build the reach already lints its
// users. Digest and Ordinal are not compared at all: a run reads every file once, so the file of one name is the same
// bytes in each of its builds, and an ordinal counts only the declarations no objectpath reaches, an init function or
// `_`, which no body uses.
func (finder *functionFinder) find(identity build_reads.Identity) (types.Object, bool) {
	home := finder.packageAt(identity.PackagePath)
	if home == nil {
		return nil, false
	}
	var object types.Object
	switch identity.Kind {
	case build_reads.IdentityObject:
		declared, found := objectAt(home, identity.Path)
		if !found {
			return nil, false
		}
		object = declared
	case build_reads.IdentityMethod:
		method, found := methodAt(home, string(identity.Path), identity.Method)
		if !found {
			return nil, false
		}
		object = method
	default:
		return nil, false
	}
	return object, finder.inIdentityFile(home, object, identity)
}

// pushedObject is the build's object for identity of an object only a wrapper fact is compared on
// (fact_reach.PushedObject): a variable or a field, found as find finds one, or a method of an interface, which find
// leaves unfound (methodAt), found here among the methods of the interface its type names, held to its file as find
// holds an object of the pass's own package. It is false when the build has none.
func (finder *functionFinder) pushedObject(identity build_reads.Identity) (types.Object, bool) {
	if identity.Kind != build_reads.IdentityMethod {
		return finder.find(identity)
	}
	home := finder.packageAt(identity.PackagePath)
	if home == nil {
		return nil, false
	}
	method, found := interfaceMethodAt(home, string(identity.Path), identity.Method)
	if !found {
		return nil, false
	}
	return method, finder.inIdentityFile(home, method, identity)
}

// inIdentityFile reports whether object, found in home, is declared in the file identity names, as find requires of an
// object of the pass's own package, and true for another package's, which is not held to its file.
func (finder *functionFinder) inIdentityFile(
	home *types.Package,
	object types.Object,
	identity build_reads.Identity,
) bool {
	if home != finder.pass.Pkg {
		return true
	}
	file := finder.pass.Fset.File(object.Pos())
	return file != nil && file.Name() == identity.File
}

// packageAt is the package at path as the pass's package sees it: itself, or the one package of that path among
// everything it imports, directly or through imports, and nil for none, or for a path two of them share, where no
// name says which the host meant.
func (finder *functionFinder) packageAt(path string) *types.Package {
	if path == finder.pass.Pkg.Path() {
		return finder.pass.Pkg
	}
	if finder.imported == nil {
		finder.imported = map[string]*types.Package{}
		seen := map[*types.Package]bool{finder.pass.Pkg: true}
		// A list walked as it grows, each package entering once.
		reached := []*types.Package{finder.pass.Pkg}
		for next := 0; next < len(reached); next++ {
			for _, imported := range reached[next].Imports() {
				if seen[imported] {
					continue
				}
				seen[imported] = true
				reached = append(reached, imported)
				if _, shared := finder.imported[imported.Path()]; shared {
					finder.imported[imported.Path()] = nil
					continue
				}
				finder.imported[imported.Path()] = imported
			}
		}
	}
	return finder.imported[path]
}

// objectAt is the object at path in home, as build_reads' renderer names one (IdentityObject): a path that is a plain
// identifier is a package-level name, unexported ones among them, which objectpath's own encoding never names, and
// any other is an objectpath.
func objectAt(home *types.Package, path objectpath.Path) (types.Object, bool) {
	if token.IsIdentifier(string(path)) {
		object := home.Scope().Lookup(string(path))
		return object, object != nil
	}
	object, pathError := objectpath.Object(home, path)
	if pathError != nil {
		return nil, false
	}
	return object, true
}

// interfaceMethodAt is the method named methodName of the interface that typeName names at home's package level, its
// own or one it embeds, as build_reads' renderer names an interface's method (IdentityMethod), and false for none, or
// for a name of no interface. The type name is looked up as objectAt looks up a package-level name, the one look of a
// host's identity the rules' guards list for it (readHostNamed), rather than a look of its own.
func interfaceMethodAt(home *types.Package, typeName string, methodName string) (*types.Func, bool) {
	object, found := objectAt(home, objectpath.Path(typeName))
	declared, isTypeName := object.(*types.TypeName)
	if !found || !isTypeName || declared.IsAlias() {
		return nil, false
	}
	declaredInterface, isInterface := declared.Type().Underlying().(*types.Interface)
	if !isInterface {
		return nil, false
	}
	for method := range declaredInterface.Methods() {
		if method.Name() == methodName {
			return method, true
		}
	}
	return nil, false
}

// methodAt is the method named methodName declared on the named type that typeName names at home's package level, as
// build_reads' renderer names one (IdentityMethod), exported or not. A generic type's methods are its origin's. An
// interface declares no method this way, so one of an interface type is not found, which is fine: the host compares
// no fact of one but its wrapper fact, which pushedObject finds it for (interfaceMethodAt).
func methodAt(home *types.Package, typeName string, methodName string) (*types.Func, bool) {
	declared, isTypeName := home.Scope().Lookup(typeName).(*types.TypeName)
	if !isTypeName || declared.IsAlias() {
		return nil, false
	}
	named, isNamed := declared.Type().(*types.Named)
	if !isNamed {
		return nil, false
	}
	origin := named.Origin()
	for index := range origin.NumMethods() {
		if method := origin.Method(index); method.Name() == methodName {
			return method, true
		}
	}
	return nil, false
}
