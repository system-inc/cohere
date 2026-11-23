package load_memo

import (
	"sync"

	"github.com/system-inc/go-types/types"
)

// Load is what one load's packages read, each package read once.
//
// The key is the *types.Package, never its path or name: a load type-checks each package once and every importer is
// given that one, while two packages may share a name, and a test variant shares its path.
type Load[Value any] struct {
	compute   func(declaring *types.Package) Value
	mutex     sync.Mutex
	byPackage map[*types.Package]func() Value
}

// Of is declaring's value, read by the first package of the load to ask and waited on by any that asks while it is
// read.
func (load *Load[Value]) Of(declaring *types.Package) Value {
	load.mutex.Lock()
	read, known := load.byPackage[declaring]
	if !known {
		read = sync.OnceValue(func() Value { return load.compute(declaring) })
		load.byPackage[declaring] = read
	}
	load.mutex.Unlock()
	return read()
}
