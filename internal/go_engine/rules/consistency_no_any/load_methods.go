package consistency_no_any

import (
	"go/token"
	"go/types"
	"runtime"
	"sync"
	"weak"
)

// loadMethods are the methods the packages of one load declare (declaredMethodsOf), each package read once however
// many packages of the load reach it. Every package holding a method candidate once indexed its whole import closure
// afresh, so a package the tree imports everywhere was read again for each, and Scope.Names, which sorts on every
// call, was nearly half of that.
//
// The key is the *types.Package, never its path or name: a load type-checks each package once and every importer is
// given that one, while two packages may share a name, and a test variant shares its path.
type loadMethods struct {
	mutex     sync.Mutex
	byPackage map[*types.Package]func() *foreignMethods
}

// of is what one package declares, read by the first package of the load to ask and waited on by any that asks while
// it is read.
func (load *loadMethods) of(declaring *types.Package) *foreignMethods {
	load.mutex.Lock()
	read, known := load.byPackage[declaring]
	if !known {
		read = sync.OnceValue(func() *foreignMethods { return declaredMethodsOf(declaring) })
		load.byPackage[declaring] = read
	}
	load.mutex.Unlock()
	return read()
}

// methodsByLoad are the loads the rule is running over, by the file set each load parsed into, which every pass of the
// load is given as pass.Fset. A run of the engine can load the tree more than once, a rename's rounds each load it
// again, so the memo is a load's rather than the process's: an entry holds its load's types, and is let go when the
// load's file set is collected. The file set is held weakly, since nothing in the types the entry holds leads back to
// it, so the entry alone never keeps a load alive.
var (
	methodsMutex  sync.Mutex
	methodsByLoad = map[weak.Pointer[token.FileSet]]*loadMethods{}
)

// methodsOf is the memo of the load whose files were parsed into fileSet.
func methodsOf(fileSet *token.FileSet) *loadMethods {
	key := weak.Make(fileSet)
	methodsMutex.Lock()
	defer methodsMutex.Unlock()
	load, known := methodsByLoad[key]
	if !known {
		load = &loadMethods{byPackage: map[*types.Package]func() *foreignMethods{}}
		methodsByLoad[key] = load
		runtime.AddCleanup(fileSet, func(collected weak.Pointer[token.FileSet]) {
			methodsMutex.Lock()
			defer methodsMutex.Unlock()
			delete(methodsByLoad, collected)
		}, key)
	}
	return load
}
