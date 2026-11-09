// Package fact_resolver computes a fact about a function only when a reader asks of it, building only that function
// and what it reaches, where a fact pass pushed through the checker learns its fact on every function of every
// dependency (#w7zmvat). One resolver serves a run: the checker tells it of each package as ctrlflow finishes there
// (checker.Options.Resolver), and it keeps that package's syntax, type information and no-return predicate past the
// checker's release, so a question about any package of the graph can be answered while the run lasts.
//
// Its program is one for the run, a package created in it when a question first reaches it, with its direct imports,
// so a build finds each callee in the package that declares it. Each package's functions build with that package's
// own no-return predicate, ctrlflow's, as build_intermediate_representation builds a package alone, so a function
// built here has the blocks it has there.
//
// The resolver knows no fact. A fact kind, as function_purity's, keeps its state in each package under its Kind and
// computes there under the package's lock, asking across a package edge only through Visible, which passes what the
// checker's inheritFacts would have carried. Locks are taken only downward along imports, so they never wait in a
// circle.
package fact_resolver

import (
	"fmt"
	"go/types"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Resolved keys, in a pass's ResultOf, the run's *Resolver, which the engine's checker hands every pass when its
// checker.Options.Resolved is this key and its Options.Resolver is set. It is a key and never runs, as rule.Timed is:
// no analyzer requires it, so it is in no graph. One that required it would fail on every package, naming it.
var Resolved = &analysis.Analyzer{
	Name: "fact_resolver",
	Doc:  "keys the resolver that computes facts on demand, which the checker hands every pass, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the resolver the checker hands "+
			"every pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}

// Resolver is one run's facts on demand: every package ctrlflow finished on, by its types, and the program their
// functions build in.
type Resolver struct {
	// packages is every package learned, a *Package by its *types.Package, read by every question and written once
	// for each package, which a sync.Map serves without a lock on the read.
	packages sync.Map

	// mutex guards program and every package's creation in it.
	mutex   sync.Mutex
	program *intermediate_representation.Program

	// counts is each kind's counts, by kind.
	counts sync.Map
}

// Kind is one fact a resolver computes, as purity is: it keys the fact's state in each package and its counts.
type Kind struct {
	Name string
}

// Counts is what a resolver computed of one kind over its run.
type Counts struct {
	// Computed is how many functions the kind judged, each once.
	Computed atomic.Int64

	// Fallbacks is how many packages the kind computed whole, the way a pushed fact is learned, because a question's
	// answer there depended on the order the push walks the package in.
	Fallbacks atomic.Int64

	// Contradicted is how many verdicts a package's fallback computed otherwise than a verdict the kind's demand walk
	// had already kept there, which is zero on a correct run, since a walk keeps a verdict only when it met no
	// order-dependent answer, and such a verdict is the push's in every order; the differential requires zero.
	Contradicted atomic.Int64
}

// New is an empty resolver, which learns each package as the checker tells it ctrlflow has finished there.
func New() *Resolver {
	return &Resolver{}
}

// Of is the resolver the checker hands pass, or an error naming what pass held instead, for a driver that set no
// checker.Options.Resolver.
func Of(pass *analysis.Pass) (*Resolver, error) {
	resolver, isResolver := pass.ResultOf[Resolved].(*Resolver)
	if !isResolver || resolver == nil {
		return nil, fmt.Errorf("reading the fact resolver on %s: the pass holds %T, where the checker's resolver was "+
			"expected (checker.Options.Resolver and Resolved, as check.DispatchOptions sets them)", pass.Pkg.Path(),
			pass.ResultOf[Resolved])
	}
	return resolver, nil
}

// Finished is the checker's word that action ran to the end. ctrlflow's action on a package is when the resolver learns
// it: its syntax, type information and ctrlflow's graphs are all in place then, and every package it imports was
// learned before it, since ctrlflow learns facts on each import first.
func (resolver *Resolver) Finished(action *checker.Action) {
	if action.Analyzer != ctrlflow.Analyzer {
		return
	}
	graphs, isGraphs := action.Result.(*ctrlflow.CFGs)
	if !isGraphs || action.Package.Types == nil {
		// ctrlflow's action finished without error, so its result is its graphs and its package has types; a package
		// without them could never be asked of, since nothing above it ran either.
		return
	}
	learned := &Package{
		resolver: resolver,
		Types:    action.Package.Types,
		imports:  make(map[*types.Package]bool, len(action.Package.Imports)),
		files:    action.Package.Syntax,
		info:     action.Package.TypesInfo,
		noReturn: graphs.NoReturn,
	}
	for _, imported := range action.Package.Imports {
		learned.imports[imported.Types] = true
	}
	resolver.mutex.Lock()
	if resolver.program == nil {
		resolver.program = intermediate_representation.NewProgram(action.Package.Fset)
	}
	resolver.mutex.Unlock()
	resolver.packages.Store(learned.Types, learned)
}

// Package is the package whose types are packageTypes, or nil when ctrlflow never finished on it, which no question
// reaches.
func (resolver *Resolver) Package(packageTypes *types.Package) *Package {
	learned, isLearned := resolver.packages.Load(packageTypes)
	if !isLearned {
		return nil
	}
	resolved, isPackage := learned.(*Package)
	if !isPackage {
		// Invariant: Finished stores only packages.
		panic(fmt.Sprintf("fact_resolver: %s is held as a %T", packageTypes.Path(), learned))
	}
	return resolved
}

// Owner is the package that declares object, where a fact on object is computed, or nil for an object of no package,
// as the error interface's Error is, which no package's facts ever held.
//
// It panics for a package the resolver never learned. On a cold run every package below one a question comes from was
// learned first, since ctrlflow ran there first; one never learned is a package the findings cache restored, with no
// syntax to build and, until #w7zmvat wave 3, no stored facts to read. Answering not pure there would silently drop
// findings, so the question fails loudly instead, the reading rule's crash on its package.
func (resolver *Resolver) Owner(object types.Object) *Package {
	if object.Pkg() == nil {
		return nil
	}
	owner := resolver.Package(object.Pkg())
	if owner == nil {
		// Invariant: with no package restored, ctrlflow learns every package below a question before the question is
		// asked. A package the findings cache restored breaks this until #w7zmvat wave 3 stores its facts, and the
		// branch wants that loud, never a silent not pure.
		panic(fmt.Sprintf("fact_resolver: asked about %s of %s, which it never learned: restored package, fact "+
			"store not built yet (#w7zmvat wave 3)", objectName(object), object.Pkg().Path()))
	}
	return owner
}

// Counts is kind's counts over the run.
func (resolver *Resolver) Counts(kind *Kind) *Counts {
	counts, _ := resolver.counts.LoadOrStore(kind, &Counts{})
	held, isCounts := counts.(*Counts)
	if !isCounts {
		// Invariant: the map holds only what LoadOrStore stored above.
		panic(fmt.Sprintf("fact_resolver: %s's counts are a %T", kind.Name, counts))
	}
	return held
}

// Visible is whether a fact on object reaches a reader in reader, as the checker's inheritFacts carries one from a
// package to its importer only when exportedFrom passes it there: the reader's own object; a method or a field from
// anywhere below; an exported function, or any other variable, a parameter among them, of a package the reader
// imports directly; a type name or a constant from anywhere below. An exported function two imports down is filtered
// out on its way up, so a reader there never holds its fact.
func Visible(reader *Package, object types.Object) bool {
	if object.Pkg() == reader.Types {
		return true
	}
	switch object := object.(type) {
	case *types.Func:
		return object.Signature().Recv() != nil || object.Exported() && reader.imports[object.Pkg()]
	case *types.Var:
		return object.IsField() || reader.imports[object.Pkg()]
	case *types.TypeName, *types.Const:
		return true
	default:
		return false
	}
}

// objectName is object as a failure names it: a function's full name, as `(*testing.T).Parallel`, and any other
// object as go/types prints it.
func objectName(object types.Object) string {
	if function, isFunction := object.(*types.Func); isFunction {
		return function.FullName()
	}
	return object.String()
}

// createLocked is packageTypes's package in the program, created if it was not: from its syntax, with its own no-return
// predicate, where ctrlflow finished on it, else from its types alone, as a package never built is. The caller holds
// the resolver's mutex.
func (resolver *Resolver) createLocked(packageTypes *types.Package) *intermediate_representation.Package {
	if existing := resolver.program.Package(packageTypes); existing != nil {
		return existing
	}
	learned := resolver.Package(packageTypes)
	if learned == nil {
		return resolver.program.CreatePackage(packageTypes, nil, nil, false)
	}
	irPackage := resolver.program.CreatePackage(packageTypes, learned.files, learned.info, false)
	irPackage.SetNoReturn(learned.noReturn)
	return irPackage
}
