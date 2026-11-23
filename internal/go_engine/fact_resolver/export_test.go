package fact_resolver

import (
	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/types/objectpath"
)

// ObjectPath is objectPath, object's objectpath by the resolver's encoder of its package, for the tests outside this
// package.
func (resolver *Resolver) ObjectPath(object types.Object) (objectpath.Path, error) {
	return resolver.objectPath(object)
}

// MeasureBuild is measureBuild, build run as Build bills a build, for the tests outside this package.
func (resolver *Resolver) MeasureBuild(build func()) {
	resolver.measureBuild(build)
}
