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
	Program   *Program          // the owning program
	Types     *types.Package    // the corresponding go/types.Package
	Members   map[string]Member // all package members keyed by name (incl. init and init#%d)
	Functions []*Function       // all functions, excluding anonymous ones
	// mapping of package objects to members (incl. methods). Contains *NamedConstant, *Global, *Function (values
	// but not types)
	values map[types.Object]Member
	init   *Function // Function("init"); the package's init function
	syntax bool      // package was loaded from syntax

	// debug is whether the package's functions record the expression map ValueForExpression reads (SetDebugMode).
	debug bool

	// The following fields are set transiently, then cleared
	// after building.
	buildOnce sync.Once   // ensures package building occurs once
	ninit     int32       // number of init functions
	info      *types.Info // package type information
	files     []*ast.File // package ASTs
	// members created as a result of building this package (includes declared functions, wrappers)
	created     []*Function
	initVersion map[ast.Expr]string // goversion to use for each global var init expr
}

// SetDebugMode sets whether irPackage's functions record the expression map, the value each expression in their
// syntax lowers to, which ValueForExpression reads. Off by default. Set it before the package builds: the builder
// records the map as it lowers, as go/ir's GlobalDebug does, and a function built without it has none.
//
// The map costs a debug reference per expression while a function builds, and an entry per expression after, so
// a package no reader asks of (a dependency, built for facts) leaves it off.
func (irPackage *Package) SetDebugMode(debug bool) {
	irPackage.debug = debug
}

// Function returns the package-level function of the specified name,
// or nil if not found.
func (irPackage *Package) Function(name string) (function *Function) {
	function, _ = irPackage.Members[name].(*Function)
	return
}

// Variable returns the package-level variable of the specified name,
// or nil if not found.
func (irPackage *Package) Variable(name string) (global *Global) {
	global, _ = irPackage.Members[name].(*Global)
	return
}

// Constant returns the package-level constant of the specified name,
// or nil if not found.
func (irPackage *Package) Constant(name string) (namedConstant *NamedConstant) {
	namedConstant, _ = irPackage.Members[name].(*NamedConstant)
	return
}

// Type returns the package-level type of the specified name,
// or nil if not found.
func (irPackage *Package) Type(name string) (namedType *Type) {
	namedType, _ = irPackage.Members[name].(*Type)
	return
}

// Build builds IR code for all functions and vars in package `irPackage`.
//
// CreatePackage must have been called for all of `irPackage`'s direct imports
// (and hence its direct imports must have been error-free). It is not
// necessary to call CreatePackage for indirect dependencies.
// Functions will be created for all necessary methods in those
// packages on demand.
//
// Build is idempotent and thread-safe.
func (irPackage *Package) Build() { irPackage.buildOnce.Do(irPackage.build) }

func (irPackage *Package) build() {
	if irPackage.info == nil {
		return // synthetic package, e.g. "testmain"
	}

	builder := builder{fns: irPackage.created}
	builder.iterate()

	// We no longer need transient information: ASTs or go/types deductions.
	irPackage.info = nil
	irPackage.created = nil
	irPackage.files = nil
	irPackage.initVersion = nil
}
