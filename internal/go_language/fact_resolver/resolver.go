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
// A package build_intermediate_representation runs on in the graph, a root the readers walk, is the exception: its
// functions build in the package that analyzer's result holds, created once by whichever of the analyzer's run and
// the resolver's first question there comes first (ReadersPackage), so the run builds a root once, not once for the
// readers and again for the facts its entry holds (#wt5vaft). A judgment there follows a call into another package
// only by the callee's object, through Ask, so the callee's own package answers, never the types-only stub the
// readers' program holds of it.
//
// The resolver knows no fact. A fact kind, as function_purity's, keeps its state in each package under its Kind and
// computes there under the package's lock, asking across a package edge only through Visible, which passes what the
// checker's inheritFacts would have carried. Locks are taken only downward along imports, so they never wait in a
// circle.
//
// A kind asks through Ask, which reads the run's store first: answers earlier runs computed, by the package's cache
// key, and the standard library's from the system cache. A package the run restored has no syntax here, so a question
// there that the store cannot answer has the run load the package from source and run again (resolver_store.go,
// #w7zmvat wave 3).
package fact_resolver

import (
	"fmt"
	"go/types"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/timing"
)

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

	// store is where answers are read for packages the run restored, and kept for the next run, or nil for a run
	// that restored nothing (see Ask, in resolver_store.go). answers memoizes every answer by answerKey,
	// storedAnswers each package's read of the store, and reloads the packages a question reached restored and
	// unanswered. kept holds, by answerKey too, every answer a kind judged inside another judgment and handed over
	// (see Keep), which no question reads: a question computes, and so records, as it would had none been kept.
	store         Store
	answers       sync.Map
	kept          sync.Map
	storedAnswers sync.Map
	reloads       sync.Map
	storeHits     atomic.Int64

	// recorded is every answer computed in a package learned, by kind's name and objectpath, as JSON, for the store,
	// under recordedLock.
	recordedLock sync.Mutex
	recorded     map[*types.Package]map[string]map[objectpath.Path][]byte

	// reads are each package's reads (see Read), under readsLock; paths memoizes each object's objectpath, which
	// encoders find (see objectPath); probing counts the probes asking now, Probe's between the analysis's waves and
	// WholeIfComplete's as the run saves.
	readsLock sync.Mutex
	reads     map[*types.Package]*readSet
	paths     sync.Map
	encoders  sync.Map
	probing   atomic.Int64

	// unsure counts, by each package's types, the unsure answers its analysis was handed (see Unsure).
	unsure sync.Map

	// held is a test's hand on the run's answers, nil in every engine run (see HoldAnswers).
	held Held

	// askers are the bodies whose rules asked each answer, nil but in a run that measures the facts' reach (see
	// RecordAskers), set before any pass runs and never after.
	askers *askerRecord

	// timing is the run's collector under `--timing`, taken from the first pass that hands the resolver over, nil for
	// a run that did not ask (see Question).
	timing atomic.Pointer[timing.Collector]
}

// The resolver's two rows under `--timing`, shared rows as the representation's build is: building functions in the
// resolver's program, and everything else a question does, judging above all. Each CPU-s is billed once: a question's
// whole cost is taken out of the asking rule's row and added to JudgeRow, and each build inside it is taken out of
// JudgeRow and added to BuildRow, as the representation's build has a row of its own.
const (
	BuildRow = "fact_resolver_build"
	JudgeRow = "fact_resolver_judge"
)

// Kind is one fact a resolver computes, as purity is: it keys the fact's state in each package and its counts.
type Kind struct {
	Name string

	// OrderBound, when set, is the kind's word on how its answers in home depend on the order a walk of home takes:
	// every function of home a read of which may answer otherwise in another order, and whether the kind's state can
	// say so, given held, the functions of home the caller holds the kind's answers about. Nil is a kind whose order
	// dependence nobody has argued yet, which a caller must take as not known whenever it holds an answer of the kind
	// about home's objects, never as order-free: saying a kind is order-free takes an argument of its own (#aswfm7a).
	// A kind whose answers hang on no order at all says so with a hook answering no functions, known.
	OrderBound func(home *Package, held []*types.Func) (bound []*types.Func, known bool)

	// TypesAlone, when set, is the kind's word that its answer about object is the same in every build that declares
	// object with those types, whatever its body: nothing of a body decides it, so no two builds' answers can differ
	// and no finding turns on one build's rather than another's. Nil says no answer of the kind is, as a hook
	// answering false says of one object, which is what a reader takes wherever nobody has argued otherwise: saying
	// an answer is its types' alone takes an argument of its own, as an order bound does.
	//
	// Neither a cgo package nor a test variant hands a reader that leaves out a marked answer with no declaration
	// (go_engine's factReachBuilder.answer) one an ordinary package would not. A function the cgo tool generates is
	// declared by its object in a file of the package, as any function is, and a package the tool translated holds no
	// record of its declarations at all, its outputs naming symbols by go:linkname, so no side is made of it. A test
	// variant is a package of its own, with its own answers and record, whose `init` functions declare no object, as
	// any package's.
	TypesAlone func(object types.Object) bool

	// RecordsEveryJudgment is the kind's word that the answers the resolver holds are a complete record of what its
	// judgments read, in every package the run computed in. It claims two things, and a kind sets it only where both
	// are argued, as an order bound is:
	//
	//   - the kind hands every verdict its judgments reach to the resolver once it is final, through Ask or Keep, a
	//     callee's it judged inside its caller's judgment among them;
	//   - a judgment of the kind reads another function's body only by way of a verdict about that function, never by
	//     looking into the body itself with no verdict made of it.
	//
	// Together they say that a function the resolver holds no answer of the kind about is one the kind never judged
	// in the run, so no answer the run holds rests on its body. A reader comparing two builds' answers has nothing to
	// compare there, where for a kind without the flag it must take such a function as one a judgment may have read
	// and left no record of (fact_reach.Settle's unrecorded rule, which that kind keeps whole). The first claim alone
	// would not do: a judgment that looked into a callee's body and made no verdict of it would leave a record as
	// complete, and rest on a body with none. A package handed an unsure answer keeps nothing (Keep, answerOf), and
	// the claim is not about it: a reader holds no answers of such a package at all. Nor is it about a package the run
	// learned that the store answered a question about, as one loaded from source to answer a question is answered from
	// its entry (Package.AnsweredFromStore): that answer was judged in another run, and nothing of what it rests on was
	// judged in this one, so a function there with no answer may be one it rests on. A reader takes no kind's record of
	// such a package as complete, and the facts' reach gives it no side (check.FactReachCapture.StoreAnswered). Nor is
	// the claim about ctrlflow's no-return of a callee, which cuts the blocks a judgment reads after a call: that is a
	// fact the checker pushes, which a reader compares on its own.
	//
	// The flag is read only of a kind registered with the resolver (Register, RecordingNames). Set on a kind that never
	// registers, as triviality and the stopping calls do not, it is inert: the kind keeps the unrecorded rule whole.
	// A kind that registers with it set joins the stub differential's kinds too (check's recordingKinds), whose guard
	// fails while the two lists differ.
	RecordsEveryJudgment bool
}

// Counts is what a resolver computed of one kind over its run.
type Counts struct {
	// Computed is how many functions the kind judged, each once.
	Computed atomic.Int64

	// Fallbacks is how many questions the kind answered by replaying the push's walk of a package, the whole package
	// or only as far as the answer needed, because the answer depended on the order the push walks the package in.
	Fallbacks atomic.Int64

	// Contradicted is how many verdicts a package's fallback computed otherwise than a verdict the kind's demand walk
	// had already kept there, which is zero on a correct run, since a walk keeps a verdict only when it met no
	// order-dependent answer, and such a verdict is the push's in every order; the differential requires zero.
	Contradicted atomic.Int64
}

// New is an empty resolver, which learns each package as the checker tells it ctrlflow has finished there, and reads
// and keeps answers in store, or in none when store is nil, for a run that restores nothing.
func New(store Store) *Resolver {
	return &Resolver{
		store:    store,
		recorded: map[*types.Package]map[string]map[objectpath.Path][]byte{},
		reads:    map[*types.Package]*readSet{},
	}
}

// Of is the resolver the checker hands pass under rule.Resolved, or an error naming what pass held instead, for a
// driver that set no checker.Options.Resolver.
func Of(pass *analysis.Pass) (*Resolver, error) {
	resolver, isResolver := pass.ResultOf[rule.Resolved].(*Resolver)
	if !isResolver || resolver == nil {
		return nil, fmt.Errorf("reading the fact resolver on %s: the pass holds %T, where the checker's resolver was "+
			"expected (checker.Options.Resolver and rule.Resolved, as check.DispatchOptions sets them)",
			pass.Pkg.Path(), pass.ResultOf[rule.Resolved])
	}
	if collector, timed := pass.ResultOf[rule.Timed].(*timing.Collector); timed && collector != nil {
		resolver.timing.CompareAndSwap(nil, collector)
	}
	return resolver, nil
}

// Question runs work, a question a rule asks, billed under `--timing` to JudgeRow and taken out of asking's row, the
// rule's listener row or its analyzer's, so the asking rule's row is its own walk alone. A question asked inside
// another is part of it and goes through no Question of its own.
func (resolver *Resolver) Question(asking string, work func()) {
	collector := resolver.timing.Load()
	if collector == nil {
		work()
		return
	}
	cpu := collector.Measure(work)
	collector.Shared(JudgeRow, cpu)
	collector.Nested(asking, cpu, 1)
}

// Build builds function, billed under `--timing` to BuildRow and taken out of JudgeRow, inside whose question it runs.
func (resolver *Resolver) Build(function *intermediate_representation.Function) {
	resolver.measureBuild(function.Build)
}

// measureBuild runs build, work that builds or creates in the resolver's program, billed as Build bills.
func (resolver *Resolver) measureBuild(build func()) {
	collector := resolver.timing.Load()
	if collector == nil {
		build()
		return
	}
	cpu := collector.Measure(build)
	collector.Shared(BuildRow, cpu)
	collector.Nested(JudgeRow, cpu, 1)
}

// Finished is the checker's word that action ran to the end. ctrlflow's action on a package is when the resolver learns
// it: its syntax, type information and ctrlflow's graphs are all in place then, and every package it imports was
// learned before it, since ctrlflow learns facts on each import first. The graph is whole by then too, so the resolver
// reads there whether build_intermediate_representation runs on the package, and whether its readers ask for the
// expression map, which a package it shares with them must record from its first build (see ReadersPackage).
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
		fileSet:  action.Package.Fset,
		files:    action.Package.Syntax,
		info:     action.Package.TypesInfo,
		noReturn: graphs.NoReturn,
	}
	for _, imported := range action.Package.Imports {
		learned.imports[imported.Types] = true
	}
	for _, sibling := range action.PackageActions() {
		if sibling.Analyzer == build_intermediate_representation.Analyzer {
			learned.forReaders = true
			learned.readsExpressions = build_intermediate_representation.ReadsExpressions(sibling.Dependents())
		}
	}
	resolver.mutex.Lock()
	if resolver.program == nil {
		resolver.program = intermediate_representation.NewProgram(action.Package.Fset)
	}
	resolver.mutex.Unlock()
	resolver.packages.Store(learned.Types, learned)
}

// ReadersPackage is the package whose types are packageTypes as build_intermediate_representation's result holds it
// for the readers, which the resolver builds its functions in too, created on the first call, by the analyzer's run
// or by the first question there, whichever comes first: or nil for a package the resolver learned no such action on,
// which builds in the resolver's own program, and which the analyzer, if it runs there in a later graph, creates
// alone. The readers' package records the expression map from its first build when its readers ask for it, as the
// analyzer creates it, so it is the same whichever creates it.
func (resolver *Resolver) ReadersPackage(packageTypes *types.Package) *intermediate_representation.Package {
	learned := resolver.Package(packageTypes)
	if learned == nil || !learned.forReaders {
		return nil
	}
	return learned.created()
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
// It panics for a package the resolver never learned, which Ask asks it only in a run with no store. There every
// package below one a question comes from was learned first, since ctrlflow ran there first; one never learned is a
// package something restored all the same, with no syntax to build and no store to read. Answering not pure there
// would silently drop findings, so the question fails loudly instead, the reading rule's crash on its package.
func (resolver *Resolver) Owner(object types.Object) *Package {
	if object.Pkg() == nil {
		return nil
	}
	owner := resolver.Package(object.Pkg())
	if owner == nil {
		// Invariant: with no package restored, ctrlflow learns every package below a question before the question is
		// asked. A package restored in a run with no store breaks this, and the branch wants that loud, never a
		// silent not pure.
		panic(fmt.Sprintf("fact_resolver: asked about %s of %s, which it never learned: a restored package, in a "+
			"run with no store of answers", objectName(object), object.Pkg().Path()))
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

// Fallbacks is every kind's Fallbacks over the run: the questions answered by replaying the push's walk of a package,
// which a run with no cache makes as much as a warm one.
func (resolver *Resolver) Fallbacks() int64 {
	var fallbacks int64
	resolver.counts.Range(func(_ any, counts any) bool {
		if held, isCounts := counts.(*Counts); isCounts {
			fallbacks += held.Fallbacks.Load()
		}
		return true
	})
	return fallbacks
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
