package intermediate_representation

import (
	"go/ast"
	"go/types"
	"runtime"
	"sync"
	"sync/atomic"
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

	// noReturn, when set, is the no-return predicate the package's functions are built with, in place of the
	// program's (SetNoReturn).
	noReturn func(*types.Func) bool

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

// SetNoReturn sets the no-return predicate irPackage's functions are built with, its anonymous ones included, in
// place of the program's (Program.SetNoReturn). Set it before any of them builds. A program whose packages each
// answer from their own control-flow facts, as ctrlflow's are per package, sets one on each, so every function
// builds as it would in a program of its package alone.
func (irPackage *Package) SetNoReturn(predicate func(*types.Func) bool) {
	irPackage.noReturn = predicate
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

	irPackage.buildCreated()

	// We no longer need transient information: ASTs or go/types deductions.
	irPackage.info = nil
	irPackage.created = nil
	irPackage.files = nil
	irPackage.initVersion = nil
}

// parallelBuildThreshold is how many created functions a package needs before Build spreads them over several
// workers. Below it the package builds on one goroutine as it always has: most packages are small, the engine
// already builds many at once, and a worker costs more than it saves there. Above it, the one package a cold run
// ends on (internal/lint/rules/core, command/cohere at cohere's root) no longer builds serially at the run's end
// (#s2z79xj). Measured, not guessed; see the task.
const parallelBuildThreshold = 128

// parallelBuildWorkers caps the workers one package's build uses, so a large package doesn't take every core
// from the packages building beside it.
const parallelBuildWorkers = 8

// buildCreated builds every function the package created, and every function their builds create in turn
// (wrappers, instantiations), each exactly once, before Build returns, so Build still leaves the package built
// whole before any reader walks it.
//
// A small package builds them on one builder, iterating to a fixed point, as go/ir's Package.Build does. A large
// one hands them to several workers, each building one created function at a time through Function.Build, which
// builds it on a builder of its own and iterates that builder to its own fixed point, so a function a build
// creates is built by the builder that created it. Each function builds under its buildOnce, so two workers
// reaching one function build it once and the later one waits; a builder waits for shared functions another
// builder is building (waitForSharedFunction), as go/ir's builders do when its packages build in parallel. The
// lazy differential holds Function.Build in any order and from many goroutines to the eager build's answer, which
// is why the two paths build the same functions.
func (irPackage *Package) buildCreated() {
	created := irPackage.created
	workers := min(runtime.GOMAXPROCS(0), parallelBuildWorkers, len(created)/(parallelBuildThreshold/2))
	if len(created) < parallelBuildThreshold || workers < 2 {
		packageBuilder := builder{fns: created}
		packageBuilder.iterate()
		return
	}
	// next is the index of the created function the next idle worker takes, so the list is shared out as the
	// workers free up, and a long function holds up only the worker that took it.
	var next atomic.Int64
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for {
				index := int(next.Add(1)) - 1
				if index >= len(created) {
					return
				}
				created[index].Build()
			}
		})
	}
	group.Wait()
}
