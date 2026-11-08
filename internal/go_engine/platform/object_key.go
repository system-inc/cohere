// Package platform is what an extra build checks beside the host's (#72n9mq0): which of a dependency's objects read
// differently under it, and which of our packages can therefore find something the host's run cannot.
package platform

import (
	"go/types"

	"golang.org/x/tools/go/types/objectpath"
)

// ObjectKey names an object of another package the same way under every build, so the host's reading of it and an
// extra build's can be compared: the path of the package declaring it, and its path inside that package as
// objectpath gives it. objectpath reaches every object one package can name in another, its exported package-level
// names, its package-level types, and their methods and fields, which is every object a reference across packages
// can resolve to.
type ObjectKey struct {
	PackagePath string
	Path        objectpath.Path
}

// KeyOf is object's key, and false for an object objectpath gives no path: a predeclared name, a package name, a
// local, or an unexported package-level name other than a type, none of which another package can name.
func KeyOf(object types.Object) (ObjectKey, bool) {
	if object == nil || object.Pkg() == nil {
		return ObjectKey{}, false
	}
	path, pathError := objectpath.For(object)
	if pathError != nil {
		return ObjectKey{}, false
	}
	return ObjectKey{PackagePath: object.Pkg().Path(), Path: path}, true
}
