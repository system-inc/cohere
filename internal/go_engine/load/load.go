// Package load is the Go engine's one type-checked read of a workspace: every package and dependency parsed and
// type-checked from source, as checker.Analyze requires, test variants included, and returned as go/packages
// would have returned them. What the load could not list, parse or type-check is the engine's types phase, so
// every such error comes back as a diagnostic, and a file the build constraints left out, which no rule will see,
// comes back named rather than silently missed.
//
// The only go commands it runs are `go env` and `go list -deps -json -e`, which read and never build, so it runs
// where a runner of prebuilt tests answers only those, as Loom's does. go/packages cannot: it always asks go list
// for -compiled, -test, -export and -pgo, and compiling is what such a runner refuses. So the test variants go
// test would build are built here (graph.go), and no cgo tool runs: a dependency's cgo file is type-checked as
// written, with package "C" faked (withheld_errors.go and cgo_reach.go say what that costs). A project with cgo
// files of its own is checked as it builds, always: one more go list, with -compiled, has the cgo tool translate
// every package with cgo files (listCompiled), and those packages are type-checked from its outputs, so that load
// does not run where go may not build. Every other package of it loads as on any other project, the findings
// cache's export data and go list's kept answer included.
//
// It takes and returns plain values; the engine turns them into contract records.
package load

import (
	"fmt"
	"go/build"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// Loaded is one load of a workspace.
type Loaded struct {
	// Packages are the ones to analyze: each package with its in-package tests ("p [p.test]") in place of the
	// package alone, each external test package ("p_test [p.test]"), and never go test's synthesized main, less any
	// Caching.Analyzes turned down. Their Imports reach the whole graph, as go/packages would have loaded it.
	Packages []*packages.Package

	// Untouched are the packages to analyze the Restorer restored untouched (Restored.Untouched), left out of
	// Packages, in the graph's order.
	Untouched []*packages.Package

	// Roots are what go/packages would return with Tests set: each package the patterns matched, and each test variant
	// go test builds of it, "p [p.test]" beside p and "p_test [p.test]", less go test's synthesized main. Packages
	// are chosen from them; a driver that analyzes every variant, as the copy of analysistest does, takes them all.
	Roots []*packages.Package

	// Diagnostics are every list, parse and type error anywhere in the graph, and every test function go test
	// would refuse, each once, sorted by file and position.
	Diagnostics []Diagnostic

	// Excluded are the workspace files the build constraints left out, each once, sorted by file: those a
	// loaded package lists as ignored, and those of the census no package holds, as in a directory whose every
	// file is left out, which go list matches as no package at all.
	Excluded []Excluded

	// Unseen are the files of the census no package holds that the build constraints would build, sorted: a
	// gap, since nothing type-checked them.
	Unseen []string

	// PackagesLoaded counts every package in the graph, dependencies and test variants included.
	PackagesLoaded int

	// WithheldInDependencyCgo are the type errors no diagnostic reports because they are in a dependency the cgo
	// tool would translate and reach its cgo files (see Withheld), one per file, sorted by file, and never nil.
	WithheldInDependencyCgo []Withheld

	// WithheldDependencyCgoDeclarations counts the declarations of dependencies with cgo files the analyzers were
	// given without what reaches cgo: functions without their bodies, package-level variables without their initial
	// values (see withholdCgoDeclarations).
	WithheldDependencyCgoDeclarations int

	// Withheld is, by package, what the load withheld of it, its own errors and declarations, for a package the
	// findings cache saves; a package it restored has what it had when it was checked.
	Withheld map[*packages.Package]PackageWithheld

	// Bodiless is what each package the load checked from source declares without a body, for the findings cache
	// to keep with each package it saves (see Bodiless).
	Bodiless *Bodiless

	// ListingKept is set when go list's answer was the one kept from an earlier run, since nothing it reads had moved
	// (see cachedList).
	ListingKept bool

	// ListedInProcess is set when the listing was read in this process rather than by go list, and GoListReason, when
	// go list read it, says what the in-process lister could not answer exactly (see listInProcess). A kept listing
	// is neither: nothing was listed.
	ListedInProcess bool
	GoListReason    string

	// FilesParsed counts the files the load parsed, and PackagesFromExportData the packages whose types it read
	// from the findings cache's export data rather than checking them.
	FilesParsed            int
	PackagesFromExportData int

	// CgoTranslated is set when a package of the project has cgo files, so go list -compiled had the cgo tool
	// translate every package with cgo files, and those were type-checked from its outputs rather than with package
	// "C" faked (see listCompiled).
	CgoTranslated bool

	// CgoTranslatedPackages counts the packages whose cgo files the cgo tool translated, and CgoTranslationElapsed is
	// how long the go list -compiled that ran it took. cgo's preprocessing is the one thing the engine has go build,
	// so every run says how much there was: both are 0 on a load that translates nothing.
	CgoTranslatedPackages int
	CgoTranslationElapsed time.Duration

	// PackagesInWorkspace counts the packages to analyze, each type-checked, those Caching.Analyzes turned down
	// included.
	PackagesInWorkspace int

	// Elapsed is how long the whole load took.
	Elapsed time.Duration

	// UnguardedWorkspaceSum is the go.work.sum a go command of the load ran beside with no side copy of the go.work,
	// since none could answer exactly as the tree's (see workspaceSumGuard), so go list may have rewritten it, or
	// empty when every one ran under a copy, or none ran. A load that fails says it in its error.
	UnguardedWorkspaceSum string

	// Inputs are what the load read to decide what it returned, for the inputs record (see inputs.go).
	Inputs Inputs

	// Reads are, by package, what every package of the graph read of other packages, for an extra build to restore it
	// by (see build_reads). The load leaves a package it checked from source unknown, for check.Run, handed the same
	// map, to walk its type reads into and add its rules' reads to, unless it knows the package can never be
	// restored: one checked for its types alone, or with cgo (recordReads). A package whose types came from export
	// data, or that the load never needed, was restored from the findings cache, and is marked never for it. Nil
	// unless Caching.RecordReads asked for them.
	Reads map[*packages.Package]*build_reads.Reads

	// ReadsRecorded counts the packages given reads, 0 for a load that records none.
	ReadsRecorded int

	// WalkSlots are the load's slots, which the rules' type-read walks take below every package's work (see
	// check.Options.WalkSlots), free once the load has ended.
	WalkSlots *WalkSlots

	// Rewritten is the bytes the load parsed each cgo output from whose `//line` directives it rewrote, which a
	// reader of a file's source reads in place of the disk's (see Rewritten).
	Rewritten *Rewritten

	// testFunctions is each test variant to analyze's refused test functions, judged as it was checked, for the
	// diagnostics, one Caching.Analyzes turned down included, since go test refuses them all the same.
	testFunctions map[*packages.Package][]Diagnostic
}

// Packages loads the packages the patterns name from directory, with the current environment plus
// environment (such as `GOWORK=off`), and accounts for census, the project's own absolute list of its .go
// files: each comes back held by a package, in Excluded, or in Unseen. It returns an error only when
// nothing could be loaded at all; what the packages themselves get wrong is Loaded.Diagnostics.
func Packages(directory string, patterns []string, environment []string, census []string) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, Caching{}, nil)
}

// PackagesWithOverlay is Packages reading the files in overlay, by absolute path as the load names them, in place of
// what is on disk, so a rewrite is type-checked before it is written. Only the parse reads the overlay. go list, the
// constraints and the census read the disk, which is right for a rewrite that changes no file's imports, package
// clause or build lines, as a rename does not: go list's answer for the overlaid tree is its answer for the tree on
// disk. A project with cgo of its own, whose cgo files the cgo tool translates from disk, takes no overlay.
func PackagesWithOverlay(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	overlay map[string][]byte,
) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, Caching{}, overlay)
}

// PackagesWithRootSyntax is PackagesWithOverlay keeping syntax and type information for the roots alone, for a
// reader of Roots and Packages that reads a dependency's types and never its syntax, as the rename does. A dependency,
// any package not among Roots, is type-checked into a types.Info with no maps, so the checker records nothing of it,
// and leaves the load with no Syntax and that empty TypesInfo; its Types, Errors, Imports and the rest are as
// PackagesWithOverlay gives them. One that withholds what reaches its cgo files keeps both until its errors are
// judged and its declarations withheld (see withholdCgoDeclarations), so what the load withholds is the same. A
// dependency's syntax and maps are most of a load's memory once the standard library is in it: its maps are never
// made, and its syntax goes when the load returns.
func PackagesWithRootSyntax(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	overlay map[string][]byte,
) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, Caching{rootSyntaxOnly: true}, overlay)
}

// Caching is what the findings cache gives a load: a Restorer, whose packages the load parses and type-checks none
// of, and a store for go list's answer, kept until something it reads moves. Either may be nil.
type Caching struct {
	Restorer Restorer
	Listings ListingStore

	// SharedReads is the directory and file reads every load of the run that lists in process shares, nil for none
	// (see SharedReads): set on the loads that follow every write of the run, the host's and each extra build's.
	SharedReads *SharedReads

	// Fingerprints is the kept listing's directory fingerprints the loads after every write of the run share, nil for
	// none (see Fingerprints): set on each extra build's load and never on the host's, since the long-line wrap writes
	// between the host's first load and the builds.
	Fingerprints *Fingerprints

	// Walks is the package directories the loads after every write of the run share, nil for none (see PackageWalks):
	// set on each extra build's load and never on the host's, as Fingerprints is.
	Walks *PackageWalks

	// Pool is the run's one set of slots, which every load and check of the run shares, the host's and every extra
	// build's, so the run never runs more work at once than it has cores (#ckm0hsp). Nil, the load has slots of its
	// own, one per core.
	Pool *bounded.Pool

	// GoEnvironment is go env's answer the run already has, from its census, which the load reads in place of asking
	// again when it is for the load's workspace and environment (see GoEnvironment).
	GoEnvironment *GoEnvironment

	// Held is the bytes the run already holds of the workspace's files, read in place of opening each again by every
	// reader of the load: the in-process lister and the kept listing's fingerprint, which read each file's header, the
	// parse, and the build constraints of the files no package holds. go list, when the in-process lister doubts, and
	// the cgo tool still read the disk, which the held bytes are. Nil reads every file from disk.
	Held Held

	// Begin, when set, is called once the graph is built and the Restorer has decided, before any package is
	// type-checked, on the load's goroutine, so the caller can start analyzing beside the load (see Overlap). Only a
	// Restorer whose decisions are all made before the first type check may be given with it, as one without a store
	// is, restoring the standard library from the binary.
	Begin func(*Overlap)

	// Timing is the run's collector under `--timing`, which the load adds its phases to: the listing, each file's read
	// and parse, each package's type check and each read of export data. Nil measures nothing (#3apgjer).
	Timing *timing.Collector

	// Analyzes, when set, says which of the packages to analyze the caller's rules analyze, from each one's ID and
	// files, before any is type-checked. One it turns down is still loaded and type-checked, for what imports it,
	// its errors are still diagnostics and it counts in PackagesInWorkspace, and it is left out of Packages and of
	// what the Restorer and Begin are given as the packages to analyze. The engine turns down a package with no file
	// in scope, so a package every file of which git ignores is never linted. Nil analyzes every one.
	Analyzes func(root *packages.Package) bool

	// RecordReads has the load give every package reads of other packages, to record into (see Loaded.Reads), which
	// only a run with extra builds to restore packages into asks for (#b2t9xmf). Unset, the load records nothing, and
	// Loaded.Reads and Overlap.Reads are nil.
	RecordReads bool

	// AnalyzedRootsOnly leaves out of the load every root no action reaches: a package whose in-package test variant
	// "p [p.test]" is analyzed in its place (analyzedOfGraph) and which no package of the graph imports, as a command
	// main with tests is. The variant holds every file of it, so nothing goes unanalyzed, and the files a load reports
	// as compiled through its Roots are the same. An extra build asks for it (#ws2ptvf): the host type-checks such a
	// package as a root of its patterns, and each build would check it again for nothing. Unset, every root is loaded.
	AnalyzedRootsOnly bool

	// WithoutTests builds no test variant of any root: the load is of the packages the patterns name, for their
	// declarations alone, as the standard library's side load under `--build-dependence` is (#ss4t5g6), where every
	// root's tests would be thousands of packages no reader wants. The listing still names what the tests import, and
	// nothing reaches it.
	WithoutTests bool

	// rootSyntaxOnly has every dependency type-checked with no type maps and left without its syntax (see
	// PackagesWithRootSyntax).
	rootSyntaxOnly bool
}

// PackagesCached is Packages with the findings cache.
func PackagesCached(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	caching Caching,
) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, caching, nil)
}

// loadWorkspace is PackagesCached, PackagesWithOverlay when overlay is set, and PackagesOverlapping when caching's
// Begin is.
// With translateOwnCgo unset it translates nothing even for a project with cgo files of its own, which only the tests
// ask for, to judge the load with package "C" faked on such a package.
func loadWorkspace(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	translateOwnCgo bool,
	caching Caching,
	overlay map[string][]byte,
) (*Loaded, error) {
	started := time.Now()
	// Closed on every return, a failed load's included, so an analysis waiting on a package that is final only at the
	// load's end never waits forever.
	ended := make(chan struct{})
	defer close(ended)
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, fmt.Errorf("resolving %s: %w", directory, absoluteError)
	}
	goEnvironment := caching.GoEnvironment
	if !goEnvironment.answers(workspace, environment) {
		var environmentError error
		measureListingCommand(caching.Timing, func() {
			goEnvironment, environmentError = ReadGoEnvironment(workspace, environment, caching.Held)
		})
		if environmentError != nil {
			return nil, environmentError
		}
	}
	loaded, loadError := loadUnder(goEnvironment, patterns, census, translateOwnCgo, caching, overlay, ended)
	unguarded := goEnvironment.guard.unguardedSum()
	if loadError != nil {
		if unguarded != "" {
			return nil, fmt.Errorf("%w; go list ran without a side copy of go.work, so it may have rewritten %s",
				loadError, unguarded)
		}
		return nil, loadError
	}
	loaded.UnguardedWorkspaceSum = unguarded
	loaded.Elapsed = time.Since(started)
	return loaded, nil
}

// loadUnder is loadWorkspace once goEnvironment is had: the listing, the cgo tool's translation when the project has
// cgo files of its own, the load from source, its diagnostics and its inputs.
func loadUnder(
	goEnvironment *GoEnvironment,
	patterns []string,
	census []string,
	translateOwnCgo bool,
	caching Caching,
	overlay map[string][]byte,
	ended <-chan struct{},
) (*Loaded, error) {
	workspace := goEnvironment.workspace
	buildContext := goEnvironment.contextFor(caching.Held)
	var listed *listing
	var listingKept bool
	var listError error
	// This goroutine's own share of the listing; every goroutine the listing starts, and every go command it waits
	// for, is measured where it runs.
	measureListing(caching.Timing, func() {
		listed, listingKept, listError = cachedList(
			goEnvironment,
			patterns,
			caching.Listings,
			caching.Held,
			caching.SharedReads,
			caching.Fingerprints,
			caching.Walks,
			caching.Timing,
		)
	})
	if listError != nil {
		return nil, fmt.Errorf("loading %s from %s: %w", strings.Join(patterns, " "), workspace, listError)
	}
	// compiled stays nil, and nothing is translated, unless the project has cgo files of its own.
	var compiled map[string]*compiledPackage
	var translationElapsed time.Duration
	if translateOwnCgo && hasOwnCgo(listed, workspace) {
		if len(overlay) > 0 {
			// The cgo tool reads its files from disk, so an overlay would prove some of the rewrite and not the rest.
			return nil, fmt.Errorf("loading %s with an overlay: the project has cgo files of its own, which the cgo "+
				"tool reads from disk", workspace)
		}
		compiledEnvironment := translatedEnvironment(goEnvironment)
		translationStarted := time.Now()
		var compiledError error
		// go list -compiled is billed to the listing, the cgo tool's compiles among its children, since it is a go list
		// the load runs and no other row would show what it cost.
		measureListingCommand(caching.Timing, func() {
			compiled, compiledError = listCompiled(goEnvironment.guard, workspace, compiledEnvironment, listed)
		})
		if compiledError != nil {
			return nil, compiledError
		}
		translationElapsed = time.Since(translationStarted)
	}
	roots, loaded, loadError := fromSource(workspace, listed, buildContext, caching, overlay, compiled, ended)
	if loadError != nil {
		return nil, loadError
	}

	if compiled != nil {
		loaded.CgoTranslated = true
		loaded.CgoTranslationElapsed = translationElapsed
		for path, each := range compiled {
			if each.translated(listed.byPath[path]) {
				loaded.CgoTranslatedPackages++
			}
		}
	}
	loaded.ListingKept = listingKept
	loaded.ListedInProcess = listed.inProcess
	loaded.GoListReason = listed.goListReason
	loaded.Roots = roots
	loaded.Diagnostics = diagnostics(workspace, roots, loaded.testFunctions)
	packages.Visit(roots, nil, func(*packages.Package) { loaded.PackagesLoaded++ })
	excluded, unseen, excludedError := excludedAndUnseen(workspace, buildContext, roots, census)
	if excludedError != nil {
		return nil, excludedError
	}
	loaded.Excluded, loaded.Unseen = excluded, unseen
	loaded.Inputs = collectInputs(workspace, goEnvironment.reported, roots, census)
	return loaded, nil
}

// hasOwnCgo reports whether a listed package inside workspace has cgo files, which go list lists only when cgo is
// on for the target.
func hasOwnCgo(listed *listing, workspace string) bool {
	for _, listedPackage := range listed.packages {
		if len(listedPackage.CgoFiles) > 0 && inside(workspace, listedPackage.Directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// fromSource parses and type-checks the listing's graph from source, each file in overlay from its bytes there, each
// other file caching holds from its held bytes, and each package compiled holds the cgo tool's translation of from its
// outputs, and returns its roots and what the load knows of them before the diagnostics and the counts. The
// declarations of dependencies that reach their cgo files lose what reaches it only once every package is
// type-checked. caching's Restorer is told which loader ran, since a translated package has other files than one
// checked with package "C" faked. caching's Begin, when set, is given the graph once the Restorer has decided and
// before the first type check, with ended, closed when the load returns, for what is final only then.
func fromSource(
	workspace string,
	listed *listing,
	buildContext *build.Context,
	caching Caching,
	overlay map[string][]byte,
	compiled map[string]*compiledPackage,
	ended <-chan struct{},
) ([]*packages.Package, *Loaded, error) {
	sizes := types.SizesFor("gc", buildContext.GOARCH)
	if sizes == nil {
		return nil, nil, fmt.Errorf("loading %s: go/types knows no sizes for GOARCH %s", workspace,
			buildContext.GOARCH)
	}
	built := newGraph(listed, workspace, compiled)
	if caching.WithoutTests {
		built.dropTestVariants()
	}
	if caching.AnalyzedRootsOnly {
		built.roots = reachedRoots(built)
	}
	withheld := newWithheldErrors()
	typeChecker := newLoader(built, sizes, withheld, overlay, caching.Held, caching.Timing)
	if caching.Pool != nil {
		typeChecker.slots = caching.Pool
	}
	reachable := typeChecker.connect()
	if caching.rootSyntaxOnly {
		for _, current := range reachable {
			typeChecker.typesOnly[current.loaded] = true
		}
		for _, root := range built.roots {
			delete(typeChecker.typesOnly, root.loaded)
		}
	}
	toAnalyze := analyzedOfGraph(built)
	// Every package to analyze has its test functions judged, one the caller's rules leave alone too: go test refuses
	// a wrongly signed test whoever reads the package.
	for _, analyzedPackage := range toAnalyze {
		typeChecker.analyzed[analyzedPackage] = true
	}
	analyzedPackages := toAnalyze
	if caching.Analyzes != nil {
		analyzedPackages = slices.DeleteFunc(slices.Clone(toAnalyze), func(root *packages.Package) bool {
			return !caching.Analyzes(root)
		})
	}
	// Made for every reachable package before any is checked, the restored ones too, whose export data may not read,
	// so the map a run beside the load is handed (Overlap.Reads) never changes once it starts.
	if caching.RecordReads {
		typeChecker.reads = make(map[*packages.Package]*build_reads.Reads, len(reachable))
		for _, current := range reachable {
			typeChecker.reads[current.loaded] = &build_reads.Reads{}
		}
	}
	if caching.Restorer != nil {
		loaderName := LoaderSource
		if compiled != nil {
			loaderName = LoaderCgoTranslated
		}
		typeChecker.restored = caching.Restorer.Restore(loaderName, loadedOf(reachable), analyzedPackages)
	}
	// A package restored untouched is analyzed by nothing, and has no say on its files.
	var untouched []*packages.Package
	analyzedPackages = slices.DeleteFunc(slices.Clone(analyzedPackages), func(analyzedPackage *packages.Package) bool {
		restored, held := typeChecker.restored[analyzedPackage]
		if held && restored.Untouched {
			untouched = append(untouched, analyzedPackage)
			return true
		}
		return false
	})
	if caching.Begin != nil {
		nodes := make(map[*packages.Package]*node, len(reachable))
		for _, current := range reachable {
			nodes[current.loaded] = current
		}
		caching.Begin(&Overlap{
			Packages:  analyzedPackages,
			Reads:     typeChecker.reads,
			Bodiless:  typeChecker.bodiless,
			WalkSlots: &WalkSlots{slots: typeChecker.slots},
			Rewritten: typeChecker.rewritten,
			nodes:     nodes,
			ended:     ended,
		})
	}
	typeChecker.load(reachable)

	roots := make([]*packages.Package, 0, len(built.roots))
	for _, root := range built.roots {
		roots = append(roots, root.loaded)
	}
	declarations := withholdCgoDeclarations(typeChecker.fileSet, reachable)
	// Only now is a dependency that withholds done with its syntax and type maps; one that withholds nothing was
	// checked without maps, and its syntax goes with the rest.
	for _, current := range reachable {
		if typeChecker.typesOnly[current.loaded] {
			current.loaded.Syntax = nil
			current.loaded.TypesInfo = &types.Info{}
		}
	}
	byPackage := map[*packages.Package]PackageWithheld{}
	distinct := map[string]bool{}
	for _, current := range reachable {
		owned := PackageWithheld{Errors: withheld.ownedBy(current.loaded), Declarations: declarations[current.loaded]}
		if restored, held := typeChecker.restored[current.loaded]; held && !typeChecker.declined[current.loaded] {
			owned.Declarations = restored.Withheld.Declarations
		}
		for _, position := range owned.Declarations {
			distinct[position] = true
		}
		if len(owned.Errors)+len(owned.Declarations) > 0 {
			byPackage[current.loaded] = owned
		}
	}
	// A package the findings cache restored has no type maps to walk, and its entry keeps no reads yet, so it is never
	// left unknown.
	for current, reads := range typeChecker.reads {
		if !typeChecker.checked[current] {
			reads.MarkNever(build_reads.RestoredFromCache)
		}
	}
	return roots, &Loaded{
		Packages:                          analyzedPackages,
		Untouched:                         untouched,
		PackagesInWorkspace:               len(toAnalyze),
		WithheldInDependencyCgo:           withheld.sorted(),
		WithheldDependencyCgoDeclarations: len(distinct),
		Withheld:                          byPackage,
		Bodiless:                          typeChecker.bodiless,
		FilesParsed:                       int(typeChecker.filesParsed.Load()),
		PackagesFromExportData:            int(typeChecker.fromExportData.Load()),
		Reads:                             typeChecker.reads,
		ReadsRecorded:                     len(typeChecker.reads),
		WalkSlots:                         &WalkSlots{slots: typeChecker.slots},
		Rewritten:                         typeChecker.rewritten,
		testFunctions:                     typeChecker.testFunctions,
	}, nil
}

// reachedRoots is built's roots less each one no action reaches: a package whose in-package test variant is
// analyzed in its place (see analyzedOfGraph) and which no package of the graph imports. A root some package imports
// stays, since connect would reach it through the importer all the same.
func reachedRoots(built *graph) []*node {
	imported := map[string]bool{}
	for _, each := range built.nodes {
		for _, identifier := range each.imports {
			imported[identifier] = true
		}
	}
	kept := make([]*node, 0, len(built.roots))
	for _, root := range built.roots {
		withTests := built.nodes[root.loaded.ID+" ["+root.loaded.ID+".test]"]
		if root.loaded.ForTest == "" && withTests != nil && !imported[root.loaded.ID] {
			continue
		}
		kept = append(kept, root)
	}
	return kept
}

// analyzedOfGraph is the roots of the load to analyze, as a driver of go/packages would choose them from its roots,
// in the graph's order: each package with tests as "p [p.test]", which holds every file of p, in place of p,
// so no file of p is analyzed and reported twice; each external test package, "p_test [p.test]"; and each package
// with no in-package tests as itself. A package go test builds again for another's tests, "q [p.test]", is no
// root: it is q, analyzed already.
func analyzedOfGraph(built *graph) []*packages.Package {
	var chosen []*packages.Package
	for _, root := range built.roots {
		withTests := built.nodes[root.loaded.ID+" ["+root.loaded.ID+".test]"]
		if root.loaded.ForTest == "" && withTests != nil {
			continue
		}
		chosen = append(chosen, root.loaded)
	}
	return chosen
}

// translatedEnvironment is the whole environment go list -compiled runs with in goEnvironment's workspace when it
// translates the cgo files of a project with cgo of its own: `GOFLAGS` as go reads it there, from the environment or
// the go env file when that is empty, with every `-trimpath` taken out and `-trimpath=false` put last. Analysis needs
// real source paths: under -trimpath go keys a cgo package's translation without its directory, so a load is served
// the translation cached for the same bytes in another directory, whose `//line` paths name that directory's files,
// gone once it is, and cgocall, which reads the raw file those paths name, fails. The value is never empty, since an
// empty `GOFLAGS` would hand go the go env file's flags, -trimpath among them. The flags are go env's answer under the
// environment as given, without the -pgo=off a load from source adds, as they always were.
func translatedEnvironment(goEnvironment *GoEnvironment) []string {
	kept := []string{}
	for _, flag := range strings.Fields(goEnvironment.answer["GOFLAGS"]) {
		name, _, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		if name != "trimpath" {
			kept = append(kept, flag)
		}
	}
	return withGoFlags(goEnvironment.fullEnvironment, append(kept, "-trimpath=false"))
}

// exists reports whether file can be read about.
func exists(file string) bool {
	_, statError := os.Stat(file)
	return statError == nil
}

// inside reports whether path is in workspace or below it.
func inside(workspace, path string) bool {
	return strings.HasPrefix(path, workspace+string(filepath.Separator))
}
