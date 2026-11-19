package fact_resolver

import (
	"go/types"

	"golang.org/x/tools/go/types/objectpath"
)

// ObjectPath is objectPath, object's objectpath by the resolver's encoder of its package, for the tests outside this
// package.
func (resolver *Resolver) ObjectPath(object types.Object) (objectpath.Path, error) {
	return resolver.objectPath(object)
}
