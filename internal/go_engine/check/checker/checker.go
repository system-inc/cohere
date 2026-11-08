// cohere:copied golang.org/x/tools@v0.50.0 go/analysis/checker/checker.go
// cohere:until x/tools resolves the TODO(adonovan) in Analyze, "opt: clear
// them earlier, so we can release large data structures like SSA sooner",
// and lets a dependency's syntax go. Then this copy goes away and check
// imports golang.org/x/tools/go/analysis/checker again.
//
// cohere: v0.50.0 is the version cohere links. go.mod pins
// v0.49.0, but that is not what links: go.work's TypeScript/tsc
// requires v0.50.0, and minimal version selection takes it. Diff this
// file against that upstream path to bump it; every change is marked
// "cohere:", and a test in internal/go_engine/copied holds every other
// line to upstream's, byte for byte.
//
// The changes are six.
//
// The first is the release. Upstream's Analyze keeps every action's
// Result until the whole graph finishes, under its own "TODO(adonovan):
// opt: clear them earlier, so we can release large data structures like
// SSA sooner" (checker.go:210-219 at v0.50.0, :213 at v0.49.0). On
// cohere's own tree that holds staticcheck's IR of every package in the
// graph at once. Here an action's Result is released when the last
// action that reads it has finished (releaseInputs).
//
// The second is bounded scheduling, which lets the release free
// anything in time (schedule, in scheduler.go beside this file).
// Upstream's execAll(roots) starts every action at once, about 950 on
// cohere's own root, so every package's syntax, types and staticcheck IR
// are live together, the collector marks all of it on every cycle, and
// the largest packages finish last. Here the actions on one package are
// a group, which waits until every action on another package that it
// inherits facts from has finished, then for one of a bounded number of
// slots, granted largest package first. It replaces an earlier change,
// prepare, which ordered each action's facts from below before its
// horizontal dependencies, so that an analyzer with a large Result and no
// facts, such as buildir, did not start on every package at once.
//
// Measured on cohere's own root under the engine's 5 GiB memory cap
// (collection off, the limit driving it), the engine alone, cold, four
// interleaved runs each, the same records in every run, at a load
// average of 35 to 41:
//
//	prepare:  10.0 to 16.0s wall, 69 to 78s user (mean 73.6),
//	          5.67 to 5.86 GB peak, 17 to 21 collections
//	schedule:  8.2 to 11.7s wall, 45 to 48s user (mean 45.9),
//	          5.45 to 5.53 GB peak, 10 collections
//
// The cap is what the engine ships, so the cap regime is the one that
// decides: at GOGC=100 a collector with twice the live heap to spare
// never works at a limit.
//
// The third is releaseSyntax: a package Analyze was not given, a
// dependency analyzed only for its facts, lets go of its syntax trees and
// type information once its last action has finished, and so does a
// package it was given when Options.Finished is set, after Finished. On cohere's root the
// dependencies' syntax and type information are 765 MB of the load's
// 1.6 GB live heap. Measured under the cap, interleaved, the same 335
// findings in every run, at a load average of 16 to 128:
//
//	without: collector 11.5 to 17.5s CPU (mean 15.7, 4 runs),
//	         36 to 48s user (mean 44.0, 6 runs)
//	with:    collector 9.5 to 16.4s CPU (mean 13.1, 4 runs),
//	         40 to 44s user (mean 41.9, 6 runs)
//
// Peak RSS is the cap's either way, about 5.6 GB, since with collection
// off the heap grows to the limit before the first cycle.
//
// The fourth is the restore, the seam the engine's per-package findings
// cache goes through. Options.Restored names the packages whose findings
// and facts the cache already holds. An action on such a package gets no
// horizontal prerequisites, so nothing on the package runs. It still
// finishes its vertical ones and inherits their facts as any action does,
// then Options.Restore adds the package's own facts from the cache, with
// RestoreObjectFact and RestorePackageFact, in place of a pass. An
// importer then inherits them as it would have inherited them from a run.
// ExportedFacts is what such an entry is written from: the facts an
// action exported that its importers read.
//
// The fifth is Options.ReadFile, the option upstream keeps unexported
// under its "TODO(adonovan): expose ReadFile", exported. The engine
// holds every file in scope as its load parsed it, and gives each pass
// those bytes, so a rule that reads the source, as the long-line rule
// reads every file, reads none from disk again. CheckedReadFile still
// guards which files a pass may read. Measured on cohere's own root,
// the engine alone, cold, two runs each at a load average of 13 to 17,
// the long-line rule's CPU went from 0.56 to 0.65s, 0.53 to 0.59s of it
// in os.ReadFile, to 0.19 to 0.20s, none of it reading a file.
//
// The sixth is ready actions, inside the packages the second change
// admits (schedule). There a goroutine started for every action on a
// package, and execAll another for each of its prerequisites, finished
// ones on imported packages included: a trace of a cold run on cohere's
// root found up to 2,312 goroutines runnable while 12 to 93 actions ran,
// and the critical chain's actions queued behind the rest. Here an action
// is queued once its horizontal prerequisites have finished, `GOMAXPROCS`
// workers run the queue, the package on the longest weighted path to the
// end of the graph first, and execOnce runs its prerequisites inline,
// where each has already finished. Measured on cohere's own root, the
// engine alone, cold, six interleaved runs each, the same records in
// every run, at a load average of 4.9 to 15.7, medians:
//
//	before: 3.91s wall, 31.0s user, 3.31s sys, lint 2.59s
//	after:  3.66s wall, 28.8s user, 3.30s sys, lint 2.40s
//
// Peak RSS, 8.4 to 8.5 GB, and involuntary context switches, 82,000 to
// 123,000, did not move.
//
// What else differs, because this module cannot import x/tools'
// internal packages:
//   - The init that gives analysistest access to Action.pass sets it in
//     the copy of go/analysis/internal in check/internal, which the copy
//     of analysistest in check/analysistest reads.
//   - The helpers from internal/analysis/driverutil that this file
//     calls (ReadFileFunc, CheckedReadFile, ResolveURL, ValidateFixes)
//     are copied unchanged into the driverutil package beside it, one
//     file to each upstream file, so here only its import path differs.
//   - codeFact's type assertion is the comma-ok form, as cohere's own
//     rules require of the code they check.

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package checker provides an analysis driver based on the
// [golang.org/x/tools/go/packages] representation of a set of
// packages and all their dependencies, as produced by
// [packages.Load].
//
// It is the core of multichecker (the multi-analyzer driver),
// singlechecker (the single-analyzer driver often used to provide a
// convenient command alongside each analyzer), and analysistest, the
// test driver.
//
// By contrast, the 'go vet' command is based on unitchecker, an
// analysis driver that uses separate analysis--analogous to separate
// compilation--with file-based intermediate results. Like separate
// compilation, it is more scalable, especially for incremental
// analysis of large code bases. Commands based on multichecker and
// singlechecker are capable of detecting when they are being invoked
// by "go vet -vettool=exe" and instead dispatching to unitchecker.
//
// Programs built using this package will, in general, not be usable
// in that way. This package is intended only for use in applications
// that invoke the analysis driver as a subroutine, and need to insert
// additional steps before or after the analysis.
//
// See the Example of how to build a complete analysis driver program.
package checker

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/types"
	"io"
	"iter"
	"log"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	// cohere: atomic counts the readers releaseInputs and releaseSyntax wait on.
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/analysis"
	// cohere: go/analysis/internal is the copy in check/internal, imported below.
	"golang.org/x/tools/go/packages"

	// cohere: x/tools' internal/analysis/driverutil, copied beside this file, and
	// go/analysis/internal, copied beside this package, since this module cannot
	// import x/tools' internal packages.
	"github.com/system-inc/cohere/internal/go_engine/check/checker/driverutil"
	"github.com/system-inc/cohere/internal/go_engine/check/internal"
)

// Options specifies options that control the analysis driver.
type Options struct {
	// These options correspond to existing flags exposed by multichecker:
	Sequential  bool      // disable parallelism
	SanityCheck bool      // check fact encoding is ok and deterministic
	FactLog     io.Writer // if non-nil, log each exported fact to it

	// TODO(adonovan): expose ReadFile so that an Overlay specified
	// in the [packages.Config] can be communicated via
	// Pass.ReadFile to each Analyzer.
	// cohere: exposed, as the TODO above asks. ReadFile, when set, is
	// what every Pass.ReadFile reads through, behind CheckedReadFile's
	// guard on which files a pass may read, so a driver that holds the
	// bytes its load parsed can serve them rather than have each reader
	// read the file from disk again. Unset, it is os.ReadFile.
	ReadFile driverutil.ReadFileFunc

	// cohere: Restored reports whether a package's analysis is restored
	// rather than run. Every action on such a package skips its pass and
	// its horizontal prerequisites, and Restore gives it the package's own
	// facts once it has inherited its prerequisites'. An error Restore
	// returns is the action's Err. Both are nil when nothing is restored.
	Restored func(*packages.Package) bool
	Restore  func(*Action) error

	// cohere: Finished, when set, is called with a package Analyze was
	// given once the last action on it has finished, and the package's
	// syntax and type information are then let go of as a dependency's
	// are (see releaseSyntax). Unset, a given package keeps them.
	Finished func(*packages.Package)

	// cohere: slots, workers and observeGroup are the scheduler's test
	// hooks (see schedule). slots, above 0, is how many packages run at once
	// in place of the default; workers, above 0, is how many actions; and
	// observeGroup is called as a package's group starts, with true, and once
	// its last action has finished, with false, before its slot is given back.
	slots        int
	workers      int
	observeGroup func(pkg *packages.Package, running bool)
}

// Graph holds the results of a round of analysis, including the graph
// of requested actions (analyzers applied to packages) plus any
// dependent actions that it was necessary to compute.
type Graph struct {
	// Roots contains the roots of the action graph.
	// Each node (a, p) in the action graph represents the
	// application of one analyzer a to one package p.
	// (A node thus corresponds to one analysis.Pass instance.)
	// Roots holds one action per element of the product
	// of the analyzers × packages arguments to Analyze,
	// in unspecified order.
	//
	// Each element of Action.Deps represents an edge in the
	// action graph: a dependency from one action to another.
	// An edge of the form (a, p) -> (a, p2) indicates that the
	// analysis of package p requires information ("facts") from
	// the same analyzer applied to one of p's dependencies, p2.
	// An edge of the form (a, p) -> (a2, p) indicates that the
	// analysis of package p requires information ("results")
	// from a different analyzer a2 applied to the same package.
	// These two kind of edges are called "vertical" and "horizontal",
	// respectively.
	Roots []*Action
}

// All returns an iterator over the action graph in depth-first postorder.
//
// Example:
//
//	for act := range graph.All() {
//		...
//	}
func (g *Graph) All() iter.Seq[*Action] {
	return func(yield func(*Action) bool) {
		forEach(g.Roots, func(act *Action) error {
			if !yield(act) {
				return io.EOF // any error will do
			}
			return nil
		}) // ignore error
	}
}

// An Action represents one unit of analysis work by the driver: the
// application of one analysis to one package. It provides the inputs
// to and records the outputs of a single analysis.Pass.
//
// Actions form a DAG, both within a package (as different analyzers
// are applied, either in sequence or parallel), and across packages
// (as dependencies are analyzed).
type Action struct {
	Analyzer    *analysis.Analyzer
	Package     *packages.Package
	IsRoot      bool // whether this is a root node of the graph
	Deps        []*Action
	Result      any   // computed result of Analyzer.run, if any (and if IsRoot)
	Err         error // error result of Analyzer.run
	Diagnostics []analysis.Diagnostic
	Duration    time.Duration // execution time of this step

	opts         *Options
	once         sync.Once
	pass         *analysis.Pass
	objectFacts  map[objectFactKey]analysis.Fact
	packageFacts map[packageFactKey]analysis.Fact

	// cohere: resultReaders counts the actions still to finish that read
	// this action's Result, its horizontal dependents. The last of them
	// releases a non-root Result (see releaseInputs).
	resultReaders atomic.Int32

	// cohere: syntax is shared by every action on this action's package,
	// so the last of them to finish can release the package's syntax
	// (see releaseSyntax).
	syntax *syntaxReaders
}

// cohere: syntaxReaders is what the actions on one package share to
// release its syntax: how many of them are still to finish, and which they
// are, since each pass holds the package's files and type information too.
type syntaxReaders struct {
	unfinished atomic.Int32
	actions    []*Action

	// given is whether the package was one Analyze was given, whose syntax
	// the caller still reads once the graph has finished, unless finished
	// is set, to take what it reads before the syntax goes.
	given    bool
	finished func(*packages.Package)
}

func (act *Action) String() string {
	return fmt.Sprintf("%s@%s", act.Analyzer, act.Package)
}

// Analyze runs the specified analyzers on the initial packages.
//
// The initial packages and all dependencies must have been loaded
// using the [packages.LoadAllSyntax] flag, Analyze may need to run
// some analyzer (those that consume and produce facts) on
// dependencies too.
//
// On success, it returns a Graph of actions whose Roots hold one
// item per (a, p) in the cross-product of analyzers and pkgs.
//
// cohere: every package below pkgs comes back without its Syntax and
// TypesInfo, which Analyze let go once it was done with them
// (releaseSyntax); pkgs themselves keep theirs, unless opts.Finished is
// set, which is called with each before it lets go of its too.
//
// If opts is nil, it is equivalent to new(Options).
func Analyze(analyzers []*analysis.Analyzer, pkgs []*packages.Package, opts *Options) (*Graph, error) {
	if opts == nil {
		opts = new(Options)
	}

	if err := analysis.Validate(analyzers); err != nil {
		return nil, err
	}

	// Construct the action graph.
	//
	// Each graph node (action) is one unit of analysis.
	// Edges express package-to-package (vertical) dependencies,
	// and analysis-to-analysis (horizontal) dependencies.
	type key struct {
		a   *analysis.Analyzer
		pkg *packages.Package
	}
	actions := make(map[key]*Action)

	var mkAction func(a *analysis.Analyzer, pkg *packages.Package) *Action
	mkAction = func(a *analysis.Analyzer, pkg *packages.Package) *Action {
		k := key{a, pkg}
		act, ok := actions[k]
		if !ok {
			act = &Action{Analyzer: a, Package: pkg, opts: opts}

			// Add a dependency on each required analyzers.
			// cohere: unless the package is restored, since then nothing on it runs.
			if opts.Restored == nil || !opts.Restored(pkg) {
				for _, req := range a.Requires {
					act.Deps = append(act.Deps, mkAction(req, pkg))
				}
			}

			// An analysis that consumes/produces facts
			// must run on the package's dependencies too.
			if len(a.FactTypes) > 0 {
				paths := make([]string, 0, len(pkg.Imports))
				for path := range pkg.Imports {
					paths = append(paths, path)
				}
				sort.Strings(paths) // for determinism
				for _, path := range paths {
					dep := mkAction(a, pkg.Imports[path])
					act.Deps = append(act.Deps, dep)
				}
			}

			actions[k] = act
		}
		return act
	}

	// Build nodes for initial packages.
	var roots []*Action
	for _, a := range analyzers {
		for _, pkg := range pkgs {
			root := mkAction(a, pkg)
			root.IsRoot = true
			roots = append(roots, root)
		}
	}

	// cohere: count each action's horizontal dependents, the readers
	// of its Result, so the last of them can release it.
	for _, act := range actions {
		for _, dep := range act.Deps {
			if dep.Package == act.Package {
				dep.resultReaders.Add(1)
			}
		}
	}

	// cohere: group the actions by package, so the last action on a
	// package that was not given can release its syntax, and the scheduler
	// can start a package's actions together. The groups, and the actions
	// in each, are in depth-first postorder from the roots, so a sequential
	// run takes them in the same order every time.
	given := make(map[*packages.Package]bool, len(pkgs))
	for _, pkg := range pkgs {
		given[pkg] = true
	}
	readers := make(map[*packages.Package]*syntaxReaders)
	var groups []*syntaxReaders
	forEach(roots, func(act *Action) error {
		shared, ok := readers[act.Package]
		if !ok {
			shared = &syntaxReaders{given: given[act.Package]}
			if shared.given {
				shared.finished = opts.Finished
			}
			readers[act.Package] = shared
			groups = append(groups, shared)
		}
		shared.unfinished.Add(1)
		shared.actions = append(shared.actions, act)
		act.syntax = shared
		return nil
	})

	// Execute the graph in parallel.
	// cohere: a bounded number of packages at once, largest first, and
	// within them only ready actions, on a bounded pool of workers, longest
	// path first (see schedule), where upstream's execAll(roots) starts
	// every action at once.
	schedule(groups, opts)

	// Ensure that only root Results are visible to caller.
	// (The others are considered temporary intermediaries.)
	// TODO(adonovan): opt: clear them earlier, so we can
	// release large data structures like SSA sooner.
	for _, act := range actions {
		if !act.IsRoot {
			act.Result = nil
		}
	}

	return &Graph{Roots: roots}, nil
}

func init() {
	// Allow analysistest to access Action.pass,
	// for the legacy analysistest.Result data type,
	// and for internal/checker.ApplyFixes to access pass.ReadFile.
	internal.ActionPass = func(x any) *analysis.Pass { return x.(*Action).pass }
}

type objectFactKey struct {
	obj types.Object
	typ reflect.Type
}

type packageFactKey struct {
	pkg *types.Package
	typ reflect.Type
}

// cohere: execAll, which started a goroutine for each action it was
// given, is gone: Analyze schedules the roots, and execOnce runs its
// prerequisites inline.

func (act *Action) exec() { act.once.Do(act.execOnce) }

func (act *Action) execOnce() {
	// Analyze dependencies.
	// cohere: inline, on this goroutine, where upstream's execAll starts one
	// for each. The scheduler starts an action only once every prerequisite
	// has finished, so each exec here returns at once from its once.Do; one
	// still unfinished, which only a caller outside the scheduler could
	// produce, runs here, or is waited on if another goroutine is running it.
	for _, dep := range act.Deps {
		dep.exec()
	}

	// cohere: however this action ends, a failed prerequisite or an
	// error from Run included, it has read all it will of its inputs, and
	// of its package's syntax. Deferred calls run last first, so the inputs
	// are released before the syntax.
	defer act.releaseSyntax()
	defer act.releaseInputs()

	// Record time spent in this node but not its dependencies.
	// In parallel mode, due to GC/scheduler contention, the
	// time is 5x higher than in sequential mode, even with a
	// semaphore limiting the number of threads here.
	// So use -debug=tp.
	t0 := time.Now()
	defer func() { act.Duration = time.Since(t0) }()

	// Report an error if any dependency failed.
	var failed []string
	for _, dep := range act.Deps {
		if dep.Err != nil {
			failed = append(failed, dep.String())
		}
	}
	if failed != nil {
		sort.Strings(failed)
		act.Err = fmt.Errorf("failed prerequisites: %s", strings.Join(failed, ", "))
		return
	}

	// Plumb the output values of the dependencies
	// into the inputs of this action.  Also facts.
	inputs := make(map[*analysis.Analyzer]any)
	act.objectFacts = make(map[objectFactKey]analysis.Fact)
	act.packageFacts = make(map[packageFactKey]analysis.Fact)
	for _, dep := range act.Deps {
		if dep.Package == act.Package {
			// Same package, different analysis (horizontal edge):
			// in-memory outputs of prerequisite analyzers
			// become inputs to this analysis pass.
			inputs[dep.Analyzer] = dep.Result
		} else if dep.Analyzer == act.Analyzer { // (always true)
			// Same analysis, different package (vertical edge):
			// serialized facts produced by prerequisite analysis
			// become available to this analysis pass.
			inheritFacts(act, dep)
		}
	}

	// cohere: a restored package's own facts come from the cache, in
	// place of a pass, on top of those it inherited (see Options.Restored).
	if act.opts.Restored != nil && act.opts.Restored(act.Package) {
		act.Err = act.opts.Restore(act)
		return
	}

	// Quick (nonexhaustive) check that the correct go/packages mode bits were used.
	// (If there were errors, all bets are off.)
	if pkg := act.Package; pkg.Errors == nil {
		if pkg.Name == "" || pkg.PkgPath == "" || pkg.Types == nil || pkg.Fset == nil || pkg.TypesSizes == nil {
			panic("packages must be loaded with packages.LoadSyntax mode")
		}
	}

	module := &analysis.Module{} // possibly empty (non nil) in go/analysis drivers.
	if mod := act.Package.Module; mod != nil {
		module = analysisModuleFromPackagesModule(mod)
	}

	// Run the analysis.
	pass := &analysis.Pass{
		Analyzer:     act.Analyzer,
		Fset:         act.Package.Fset,
		Files:        act.Package.Syntax,
		OtherFiles:   act.Package.OtherFiles,
		IgnoredFiles: act.Package.IgnoredFiles,
		Pkg:          act.Package.Types,
		TypesInfo:    act.Package.TypesInfo,
		TypesSizes:   act.Package.TypesSizes,
		TypeErrors:   act.Package.TypeErrors,
		Module:       module,

		ResultOf: inputs,
		Report: func(d analysis.Diagnostic) {
			// Assert that SuggestedFixes are well formed.
			if err := driverutil.ValidateFixes(act.Package.Fset, act.Analyzer, d.SuggestedFixes); err != nil {
				panic(err)
			}
			act.Diagnostics = append(act.Diagnostics, d)
		},
		ImportObjectFact:  act.ObjectFact,
		ExportObjectFact:  act.exportObjectFact,
		ImportPackageFact: act.PackageFact,
		ExportPackageFact: act.exportPackageFact,
		AllObjectFacts:    act.AllObjectFacts,
		AllPackageFacts:   act.AllPackageFacts,
	}
	readFile := os.ReadFile
	// cohere: Options.ReadFile, the field upstream's TODO asks to expose.
	if act.opts.ReadFile != nil {
		readFile = act.opts.ReadFile
	}
	pass.ReadFile = driverutil.CheckedReadFile(pass, readFile)
	act.pass = pass

	act.Result, act.Err = func() (any, error) {
		if act.Package.IllTyped && !pass.Analyzer.RunDespiteErrors {
			return nil, fmt.Errorf("analysis skipped due to errors in package")
		}

		result, err := pass.Analyzer.Run(pass)
		if err != nil {
			return nil, err
		}

		// correct result type?
		if got, want := reflect.TypeOf(result), pass.Analyzer.ResultType; got != want {
			return nil, fmt.Errorf(
				"internal error: on package %s, analyzer %s returned a result of type %v, but declared ResultType %v",
				pass.Pkg.Path(), pass.Analyzer, got, want)
		}

		// resolve diagnostic URLs
		for i := range act.Diagnostics {
			url, err := driverutil.ResolveURL(act.Analyzer, act.Diagnostics[i])
			if err != nil {
				return nil, err
			}
			act.Diagnostics[i].URL = url
		}
		return result, nil
	}()

	// Help detect (disallowed) calls after Run.
	pass.ExportObjectFact = nil
	pass.ExportPackageFact = nil
}

// cohere: releaseInputs lets go of what act read from its prerequisites
// once it has finished, so that a large intermediate Result, such as
// staticcheck's IR of a package, is collected as soon as nothing will
// read it again, rather than when the whole graph finishes.
//
// The pass's ResultOf map is cleared rather than the pass dropped, so
// the guards against fact exports after Run keep working; the map is
// the pass's only reference to its inputs. A root's own Result is kept,
// as upstream keeps it. Facts are untouched: they travel along vertical
// edges, which are not counted as readers.
func (act *Action) releaseInputs() {
	if act.pass != nil {
		act.pass.ResultOf = nil
	}
	for _, dep := range act.Deps {
		if dep.Package == act.Package && dep.resultReaders.Add(-1) == 0 && !dep.IsRoot {
			dep.Result = nil
		}
	}
}

// cohere: releaseSyntax lets go of a package's syntax trees and type
// information once the last action on it has finished, unless Analyze was
// given the package and Options.Finished is unset. A dependency is analyzed
// only by the analyzers that learn facts and what they require, and once
// those have finished its facts are all that is read of it again: an
// importer reads its types, which stay. A given package is the same to its
// importers, whose actions read their own syntax and its facts and types,
// never its syntax, so once Finished has taken what the caller reads of it
// it goes too. Every finding is the same, since every action ran as it
// would have.
//
// The actions' passes are cleared with the package, since each holds its
// files and type information. A Result that holds syntax, such as
// ctrlflow's or buildir's, is released by releaseInputs when its last
// reader, an action on the same package, has finished, so by now.
func (act *Action) releaseSyntax() {
	if act.syntax.unfinished.Add(-1) != 0 {
		return
	}
	if act.syntax.given {
		if act.syntax.finished == nil {
			return
		}
		act.syntax.finished(act.Package)
	}
	for _, finished := range act.syntax.actions {
		if finished.pass != nil {
			finished.pass.Files = nil
			finished.pass.TypesInfo = nil
		}
	}
	act.Package.Syntax = nil
	act.Package.TypesInfo = nil
}

// inheritFacts populates act.facts with
// those it obtains from its dependency, dep.
func inheritFacts(act, dep *Action) {
	for key, fact := range dep.objectFacts {
		// Filter out facts related to objects
		// that are irrelevant downstream
		// (equivalently: not in the compiler export data).
		if !exportedFrom(key.obj, dep.Package.Types) {
			if false {
				log.Printf("%v: discarding %T fact from %s for %s: %s", act, fact, dep, key.obj, fact)
			}
			continue
		}

		// Optionally serialize/deserialize fact
		// to verify that it works across address spaces.
		if act.opts.SanityCheck {
			encodedFact, err := codeFact(fact)
			if err != nil {
				log.Panicf("internal error: encoding of %T fact failed in %v: %v", fact, act, err)
			}
			fact = encodedFact
		}

		if false {
			log.Printf("%v: inherited %T fact for %s: %s", act, fact, key.obj, fact)
		}
		act.objectFacts[key] = fact
	}

	for key, fact := range dep.packageFacts {
		// TODO: filter out facts that belong to
		// packages not mentioned in the export data
		// to prevent side channels.
		//
		// The Pass.All{Object,Package}Facts accessors expose too much:
		// all facts, of all types, for all dependencies in the action
		// graph. Not only does the representation grow quadratically,
		// but it violates the separate compilation paradigm, allowing
		// analysis implementations to communicate with indirect
		// dependencies that are not mentioned in the export data.
		//
		// It's not clear how to fix this short of a rather expensive
		// filtering step after each action that enumerates all the
		// objects that would appear in export data, and deletes
		// facts associated with objects not in this set.

		// Optionally serialize/deserialize fact
		// to verify that it works across address spaces
		// and is deterministic.
		if act.opts.SanityCheck {
			encodedFact, err := codeFact(fact)
			if err != nil {
				log.Panicf("internal error: encoding of %T fact failed in %v: %v", fact, act, err)
			}
			fact = encodedFact
		}

		if false {
			log.Printf("%v: inherited %T fact for %s: %s", act, fact, key.pkg.Path(), fact)
		}
		act.packageFacts[key] = fact
	}
}

// codeFact encodes then decodes a fact,
// just to exercise that logic.
func codeFact(fact analysis.Fact) (analysis.Fact, error) {
	// We encode facts one at a time.
	// A real modular driver would emit all facts
	// into one encoder to improve gob efficiency.
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(fact); err != nil {
		return nil, err
	}

	// Encode it twice and assert that we get the same bits.
	// This helps detect nondeterministic Gob encoding (e.g. of maps).
	var buf2 bytes.Buffer
	if err := gob.NewEncoder(&buf2).Encode(fact); err != nil {
		return nil, err
	}
	if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
		return nil, fmt.Errorf("encoding of %T fact is nondeterministic", fact)
	}

	// cohere: the comma-ok form, as the house asserts. A new value of a
	// fact's type is always a fact, so the miss is never taken.
	new, isFact := reflect.New(reflect.TypeOf(fact).Elem()).Interface().(analysis.Fact)
	if !isFact {
		return nil, fmt.Errorf("decoding a %T fact: a new value of its type is not a fact", fact)
	}
	if err := gob.NewDecoder(&buf).Decode(new); err != nil {
		return nil, err
	}
	return new, nil
}

// exportedFrom reports whether obj may be visible to a package that imports pkg.
// This includes not just the exported members of pkg, but also unexported
// constants, types, fields, and methods, perhaps belonging to other packages,
// that find there way into the API.
// This is an overapproximation of the more accurate approach used by
// gc export data, which walks the type graph, but it's much simpler.
//
// TODO(adonovan): do more accurate filtering by walking the type graph.
func exportedFrom(obj types.Object, pkg *types.Package) bool {
	switch obj := obj.(type) {
	case *types.Func:
		return obj.Exported() && obj.Pkg() == pkg ||
			obj.Signature().Recv() != nil
	case *types.Var:
		if obj.IsField() {
			return true
		}
		// we can't filter more aggressively than this because we need
		// to consider function parameters exported, but have no way
		// of telling apart function parameters from local variables.
		return obj.Pkg() == pkg
	case *types.TypeName, *types.Const:
		return true
	}
	return false // Nil, Builtin, Label, or PkgName
}

// ObjectFact retrieves a fact associated with obj,
// and returns true if one was found.
// Given a value ptr of type *T, where *T satisfies Fact,
// ObjectFact copies the value to *ptr.
//
// See documentation at ImportObjectFact field of [analysis.Pass].
func (act *Action) ObjectFact(obj types.Object, ptr analysis.Fact) bool {
	if obj == nil {
		panic("nil object")
	}
	key := objectFactKey{obj, factType(ptr)}
	if v, ok := act.objectFacts[key]; ok {
		reflect.ValueOf(ptr).Elem().Set(reflect.ValueOf(v).Elem())
		return true
	}
	return false
}

// exportObjectFact implements Pass.ExportObjectFact.
func (act *Action) exportObjectFact(obj types.Object, fact analysis.Fact) {
	if act.pass.ExportObjectFact == nil {
		log.Panicf("%s: Pass.ExportObjectFact(%s, %T) called after Run", act, obj, fact)
	}

	if obj.Pkg() != act.Package.Types {
		log.Panicf("internal error: in analysis %s of package %s: Fact.Set(%s, %T): can't set facts on objects belonging another package",
			act.Analyzer, act.Package, obj, fact)
	}

	key := objectFactKey{obj, factType(fact)}
	act.objectFacts[key] = fact // clobber any existing entry
	if log := act.opts.FactLog; log != nil {
		objstr := types.ObjectString(obj, (*types.Package).Name)
		fmt.Fprintf(log, "%s: object %s has fact %s\n",
			act.Package.Fset.Position(obj.Pos()), objstr, fact)
	}
}

// cohere: ExportedFacts returns the facts act exported itself that an
// importer inherits from it, the object facts filtered as inheritFacts
// filters them, so a restore that adds them back gives every importer
// what a run would have.
func (act *Action) ExportedFacts() ([]analysis.ObjectFact, []analysis.PackageFact) {
	var objectFacts []analysis.ObjectFact
	for key, fact := range act.objectFacts {
		if key.obj.Pkg() == act.Package.Types && exportedFrom(key.obj, act.Package.Types) {
			objectFacts = append(objectFacts, analysis.ObjectFact{Object: key.obj, Fact: fact})
		}
	}
	var packageFacts []analysis.PackageFact
	for key, fact := range act.packageFacts {
		if key.pkg == act.Package.Types {
			packageFacts = append(packageFacts, analysis.PackageFact{Package: key.pkg, Fact: fact})
		}
	}
	return objectFacts, packageFacts
}

// cohere: RestoreObjectFact gives a restored action a fact its package
// exported in the run the cache holds, obj being of act's own package.
func (act *Action) RestoreObjectFact(obj types.Object, fact analysis.Fact) {
	act.objectFacts[objectFactKey{obj, factType(fact)}] = fact
}

// cohere: RestorePackageFact gives a restored action a fact its package
// exported about itself in the run the cache holds.
func (act *Action) RestorePackageFact(fact analysis.Fact) {
	act.packageFacts[packageFactKey{act.Package.Types, factType(fact)}] = fact
}

// AllObjectFacts returns a new slice containing all object facts of
// the analysis's FactTypes in unspecified order.
//
// See documentation at AllObjectFacts field of [analysis.Pass].
func (act *Action) AllObjectFacts() []analysis.ObjectFact {
	facts := make([]analysis.ObjectFact, 0, len(act.objectFacts))
	for k, fact := range act.objectFacts {
		facts = append(facts, analysis.ObjectFact{Object: k.obj, Fact: fact})
	}
	return facts
}

// PackageFact retrieves a fact associated with package pkg,
// which must be this package or one of its dependencies.
//
// See documentation at ImportObjectFact field of [analysis.Pass].
func (act *Action) PackageFact(pkg *types.Package, ptr analysis.Fact) bool {
	if pkg == nil {
		panic("nil package")
	}
	key := packageFactKey{pkg, factType(ptr)}
	if v, ok := act.packageFacts[key]; ok {
		reflect.ValueOf(ptr).Elem().Set(reflect.ValueOf(v).Elem())
		return true
	}
	return false
}

// exportPackageFact implements Pass.ExportPackageFact.
func (act *Action) exportPackageFact(fact analysis.Fact) {
	if act.pass.ExportPackageFact == nil {
		log.Panicf("%s: Pass.ExportPackageFact(%T) called after Run", act, fact)
	}

	key := packageFactKey{act.pass.Pkg, factType(fact)}
	act.packageFacts[key] = fact // clobber any existing entry
	if log := act.opts.FactLog; log != nil {
		fmt.Fprintf(log, "%s: package %s has fact %s\n",
			act.Package.Fset.Position(act.pass.Files[0].Pos()), act.pass.Pkg.Path(), fact)
	}
}

func factType(fact analysis.Fact) reflect.Type {
	t := reflect.TypeOf(fact)
	if t.Kind() != reflect.Pointer {
		log.Fatalf("invalid Fact type: got %T, want pointer", fact)
	}
	return t
}

// AllPackageFacts returns a new slice containing all package
// facts of the analysis's FactTypes in unspecified order.
//
// See documentation at AllPackageFacts field of [analysis.Pass].
func (act *Action) AllPackageFacts() []analysis.PackageFact {
	facts := make([]analysis.PackageFact, 0, len(act.packageFacts))
	for k, fact := range act.packageFacts {
		facts = append(facts, analysis.PackageFact{Package: k.pkg, Fact: fact})
	}
	return facts
}

// forEach is a utility function for traversing the action graph. It
// applies function f to each action in the graph reachable from
// roots, in depth-first postorder. If any call to f returns an error,
// the traversal is aborted and ForEach returns the error.
func forEach(roots []*Action, f func(*Action) error) error {
	seen := make(map[*Action]bool)
	var visitAll func(actions []*Action) error
	visitAll = func(actions []*Action) error {
		for _, act := range actions {
			if !seen[act] {
				seen[act] = true
				if err := visitAll(act.Deps); err != nil {
					return err
				}
				if err := f(act); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visitAll(roots)
}

func analysisModuleFromPackagesModule(mod *packages.Module) *analysis.Module {
	if mod == nil {
		return nil
	}

	var modErr *analysis.ModuleError
	if mod.Error != nil {
		modErr = &analysis.ModuleError{
			Err: mod.Error.Err,
		}
	}

	return &analysis.Module{
		Path:      mod.Path,
		Version:   mod.Version,
		Replace:   analysisModuleFromPackagesModule(mod.Replace),
		Time:      mod.Time,
		Main:      mod.Main,
		Indirect:  mod.Indirect,
		Dir:       mod.Dir,
		GoMod:     mod.GoMod,
		GoVersion: mod.GoVersion,
		Error:     modErr,
	}
}
