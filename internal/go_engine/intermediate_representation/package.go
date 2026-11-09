package intermediate_representation

import (
	"go/ast"
	"go/types"
	"sync"
)

// A Package is a single analyzed Go package containing Members for
// all package-level functions, variables, constants and types it
// declares.  These may be accessed directly via Members, or via the
// type-specific accessor methods Function, Type, Variable and Constant.
//
// Members also contains entries for "init" (the synthetic package
// initializer) and "init#%d", the nth declared init function,
// and unspecified other things too.
type Package struct {
	Program   *Program                // the owning program
	Types     *types.Package          // the corresponding go/types.Package
	Members   map[string]Member       // all package members keyed by name (incl. init and init#%d)
	Functions []*Function             // all functions, excluding anonymous ones
	values    map[types.Object]Member // mapping of package objects to members (incl. methods). Contains *NamedConstant, *Global, *Function (values but not types)
	init      *Function               // Function("init"); the package's init function
	syntax    bool                    // package was loaded from syntax

	// The following fields are set transiently, then cleared
	// after building.
	buildOnce   sync.Once           // ensures package building occurs once
	ninit       int32               // number of init functions
	info        *types.Info         // package type information
	files       []*ast.File         // package ASTs
	created     []*Function         // members created as a result of building this package (includes declared functions, wrappers)
	initVersion map[ast.Expr]string // goversion to use for each global var init expr
}

// Function returns the package-level function of the specified name,
// or nil if not found.
func (goEnginePackage *Package) Function(name string) (function *Function) {
	function, _ = goEnginePackage.Members[name].(*Function)
	return
}

// Variable returns the package-level variable of the specified name,
// or nil if not found.
func (goEnginePackage *Package) Variable(name string) (global *Global) {
	global, _ = goEnginePackage.Members[name].(*Global)
	return
}

// Constant returns the package-level constant of the specified name,
// or nil if not found.
func (goEnginePackage *Package) Constant(name string) (namedConstant *NamedConstant) {
	namedConstant, _ = goEnginePackage.Members[name].(*NamedConstant)
	return
}

// Type returns the package-level type of the specified name,
// or nil if not found.
func (goEnginePackage *Package) Type(name string) (namedType *Type) {
	namedType, _ = goEnginePackage.Members[name].(*Type)
	return
}

// Build builds IR code for all functions and vars in package `goEnginePackage`.
//
// CreatePackage must have been called for all of `goEnginePackage`'s direct imports
// (and hence its direct imports must have been error-free). It is not
// necessary to call CreatePackage for indirect dependencies.
// Functions will be created for all necessary methods in those
// packages on demand.
//
// Build is idempotent and thread-safe.
func (goEnginePackage *Package) Build() { goEnginePackage.buildOnce.Do(goEnginePackage.build) }

func (goEnginePackage *Package) build() {
	if goEnginePackage.info == nil {
		return // synthetic package, e.g. "testmain"
	}

	builder := builder{fns: goEnginePackage.created}
	builder.iterate()

	// We no longer need transient information: ASTs or go/types deductions.
	goEnginePackage.info = nil
	goEnginePackage.created = nil
	goEnginePackage.files = nil
	goEnginePackage.initVersion = nil
}
