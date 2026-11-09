package fact_resolver

import (
	"fmt"
	"go/ast"
	"go/types"
	"sync"
	"sync/atomic"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Package is one package as the resolver holds it: its types and what its functions build from, kept from the moment
// ctrlflow finished on it.
type Package struct {
	resolver *Resolver
	Types    *types.Package

	// imports is the package's direct imports, by their types, which a fact on another package's exported function
	// crosses into it from (see Visible).
	imports map[*types.Package]bool

	files    []*ast.File
	info     *types.Info
	noReturn func(*types.Func) bool

	// irPackage is the package in the resolver's program, nil until a question first reaches it, and then created
	// with each of its direct imports. Set under the resolver's mutex.
	irPackage atomic.Pointer[intermediate_representation.Package]

	// lock is held while a fact kind computes in the package (see Lock).
	lock sync.Mutex

	// states is each kind's state in the package, a pointer to the kind's own type by its *Kind (see StateOf).
	states sync.Map
}

// Lock takes the package's lock, under which a kind computes in it. A computation holding it takes another package's
// only for a package below it, one it imports, so no two ever wait on each other.
func (resolved *Package) Lock() {
	resolved.lock.Lock()
}

// Unlock gives the package's lock back.
func (resolved *Package) Unlock() {
	resolved.lock.Unlock()
}

// Files is the package's syntax, as the load parsed it, kept past the checker's release.
func (resolved *Package) Files() []*ast.File {
	return resolved.files
}

// Info is the package's type information, kept past the checker's release.
func (resolved *Package) Info() *types.Info {
	return resolved.info
}

// Function is the package's declared function or method whose object is function, built, or nil when function is no
// declared function of the package, as an instantiated method's object never is.
func (resolved *Package) Function(function *types.Func) *intermediate_representation.Function {
	irPackage := resolved.created()
	declared := resolved.resolver.program.FunctionValue(function)
	if declared == nil || declared.Package != irPackage || declared.Object() != types.Object(function) {
		return nil
	}
	declared.Build()
	return declared
}

// IntermediateRepresentation is the package in the resolver's program, created with its direct imports if it was not.
func (resolved *Package) IntermediateRepresentation() *intermediate_representation.Package {
	return resolved.created()
}

// SourceFunctions is the package's declared functions and methods, its synthetic initializer first, each built, in the
// order buildir's SrcFuncs lists them, without the anonymous ones.
func (resolved *Package) SourceFunctions() []*intermediate_representation.Function {
	irPackage := resolved.created()
	for _, function := range irPackage.Functions {
		function.Build()
	}
	return irPackage.Functions
}

// created is the package in the resolver's program, created on the first call with every package it imports, each
// from its syntax where ctrlflow finished on it, as a build of its functions requires.
func (resolved *Package) created() *intermediate_representation.Package {
	if irPackage := resolved.irPackage.Load(); irPackage != nil {
		return irPackage
	}
	resolver := resolved.resolver
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	if irPackage := resolved.irPackage.Load(); irPackage != nil {
		return irPackage
	}
	for _, imported := range resolved.Types.Imports() {
		resolver.createLocked(imported)
	}
	irPackage := resolver.createLocked(resolved.Types)
	resolved.irPackage.Store(irPackage)
	return irPackage
}

// StateOf is kind's state in resolved, a new State on the first call, which the kind fills as it computes under the
// package's lock. Every call for one kind names the same State.
func StateOf[State any](resolved *Package, kind *Kind) *State {
	state, _ := resolved.states.LoadOrStore(kind, new(State))
	held, isState := state.(*State)
	if !isState {
		// Invariant: a kind's state is made only here, as the one State its every call names.
		panic(fmt.Sprintf("fact_resolver: %s's state in %s is a %T", kind.Name, resolved.Types.Path(), state))
	}
	return held
}
