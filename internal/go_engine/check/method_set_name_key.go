package check

import (
	"go/token"

	"github.com/system-inc/go-types/types"
)

// methodSetNameKey is a method's or field's name as a method set tells names apart, go/types' Id (object.go:76 at
// go1.27) held as two strings rather than joined into one, so no key allocates: an exported name alone, and an
// unexported one with its package's path, "_" for no package or an empty path, so two packages' unexported names of
// one spelling are two names, each in the set.
type methodSetNameKey struct {
	path string
	name string
}

// methodSetNameKeyOf is object's key, a method's or a field's.
func methodSetNameKeyOf(object types.Object) methodSetNameKey {
	if token.IsExported(object.Name()) {
		return methodSetNameKey{name: object.Name()}
	}
	if object.Pkg() == nil || object.Pkg().Path() == "" {
		return methodSetNameKey{path: "_", name: object.Name()}
	}
	return methodSetNameKey{path: object.Pkg().Path(), name: object.Name()}
}
