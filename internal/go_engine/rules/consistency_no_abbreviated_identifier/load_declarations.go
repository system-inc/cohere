package consistency_no_abbreviated_identifier

import (
	"go/token"
	"go/types"
	"runtime"
	"sync"
	"weak"
)

// loadDeclarations are what the packages of one load declare (declaredMethodsOf), each package read once however many
// packages of the load reach it. A package the whole tree imports, as fmt is, was read once for every importer, and
// Scope.Names, which sorts on every call, was nearly half the rule's time.
//
// The key is the *types.Package, never its path or name: a load type-checks each package once and every importer is
// given that one, while two packages may share a name, and a test variant shares its path.
type loadDeclarations struct {
	mutex     sync.Mutex
	byPackage map[*types.Package]func() map[string][]foreignMethod
}

// of is what one package declares, read by the first package of the load to ask and waited on by any that asks while
// it is read.
func (declarations *loadDeclarations) of(declaring *types.Package) map[string][]foreignMethod {
	declarations.mutex.Lock()
	read, known := declarations.byPackage[declaring]
	if !known {
		read = sync.OnceValue(func() map[string][]foreignMethod { return declaredMethodsOf(declaring) })
		declarations.byPackage[declaring] = read
	}
	declarations.mutex.Unlock()
	return read()
}

// declarationsByLoad are the loads the rule is running over, by the file set each load parsed into, which every pass
// of the load is given as pass.Fset. A run of the engine can load the tree more than once, a rename's rounds each load
// it again, so the memo is a load's rather than the process's: an entry holds its load's types, and is let go when the
// load's file set is collected. The file set is held weakly, since nothing in the types the entry holds leads back
// to it, so the entry alone never keeps a load alive.
var (
	declarationsMutex  sync.Mutex
	declarationsByLoad = map[weak.Pointer[token.FileSet]]*loadDeclarations{}
)

// declarationsOf is the memo of the load whose files were parsed into fileSet.
func declarationsOf(fileSet *token.FileSet) *loadDeclarations {
	key := weak.Make(fileSet)
	declarationsMutex.Lock()
	defer declarationsMutex.Unlock()
	declarations, known := declarationsByLoad[key]
	if !known {
		declarations = &loadDeclarations{byPackage: map[*types.Package]func() map[string][]foreignMethod{}}
		declarationsByLoad[key] = declarations
		runtime.AddCleanup(fileSet, func(collected weak.Pointer[token.FileSet]) {
			declarationsMutex.Lock()
			defer declarationsMutex.Unlock()
			delete(declarationsByLoad, collected)
		}, key)
	}
	return declarations
}
