package go_origin_names

import (
	"go/types"
	"strings"
)

// tableEntry names what one call produces, the error of a standard library call or the context a call returns.
type tableEntry struct {
	packagePath string
	// receiver is the receiver's type name for a method, "" for a package function.
	receiver string
	name     string
	// namePrefix matches every function whose name starts with name (strconv.Parse*).
	namePrefix bool
	// newName is the value's name; for an error, "" marks a constructed error, which is named by meaning in the waves.
	newName string
}

// matches reports whether function, called on a receiver of one of receivers' types, is this entry's call.
// A method matches on either its declared receiver or the receiver expression's type, so a method promoted
// from an embedded type still matches the type it was called on: `(*testing.T).Context` is declared on the
// unexported testing.common.
func (entry tableEntry) matches(function *types.Func, receivers []TypeName) bool {
	if function.Pkg() == nil {
		return false
	}
	if function.Name() != entry.name && !(entry.namePrefix && strings.HasPrefix(function.Name(), entry.name)) {
		return false
	}
	if entry.receiver == "" {
		return len(receivers) == 0 && function.Pkg().Path() == entry.packagePath
	}
	for _, receiver := range receivers {
		if receiver == (TypeName{PackagePath: entry.packagePath, Name: entry.receiver}) {
			return true
		}
	}
	return false
}
