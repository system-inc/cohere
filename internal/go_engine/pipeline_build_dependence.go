package go_engine

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// hostDependence is the host's graph of its package-level declarations under `--build-dependence` (#qkq4gx5), built
// once from its load for every extra build to be judged on, and what each judgment compares a build's load with: the
// host's files of every package as written, and its path, by package ID, the load it was built from, and how long it
// took. restored are the packages the host read from export data, the binary's standard-library table's or the
// store's, by package ID, which the graph does not hold, and standardLibrary is the graph of the standard library's
// among them, nil when it restored none (#ss4t5g6).
type hostDependence struct {
	graph *build_dependence.Graph
	from  *load.Loaded
	// facts are the host's facts by object and kind (indexHostFacts), indexed once its lint has run, before any extra
	// build starts, nil until then.
	facts           map[factKey]check.ExportedFact
	files           map[string][]string
	paths           map[string]string
	elapsed         time.Duration
	restored        map[string]bool
	standardLibrary *standardLibraryDependence
	// restoredAnswers are the answers the host's store holds of each package it restored, by package ID, indexed with
	// its facts (indexHostFacts), which the facts census reads by objectpath.
	restoredAnswers map[string]*fact_resolver.Answers
}

// standardLibraryDependence is the standard library's own graph (#ss4t5g6): the host restores the standard library
// from the binary's table, with no syntax, so its graph holds none of it, and each build's standard library is judged
// on a graph of its own, loaded from source beside the host's lint, whose judgment answers for the objects the host's
// units read of it (build_dependence.Counterparts). done is closed once the side load and the graph have ended; graph
// is then nil when the load failed, which problem says, files are each side-loaded package's files as written, by
// package ID, which each build's are compared with, and fileSet is the side load's, where its objects' positions are,
// by which the facts census keys the host's facts of a restored package (indexHostFacts). Its packages are counted
// among the dependencies.
type standardLibraryDependence struct {
	done         chan struct{}
	graph        *build_dependence.Graph
	files        map[string][]string
	fileSet      *token.FileSet
	packages     int
	problem      error
	loadElapsed  time.Duration
	graphElapsed time.Duration
}

// buildDependenceGraph builds the host's graph from loaded, every package it checked from source, on the run's pool,
// while their syntax and type information are whole: the rules let go of each package's as they finish with it, so
// this runs between the load's end and the first rules' start, the rename's pass's or the lint phase's, which a run
// judging dependence never overlaps with its load (lintOverlaps). A package with a file in scope is the workspace's,
// its test variants included, and every other a dependency. It builds nothing unless the run judges dependence and the
// lint phase has extra builds to judge, over a load that will be linted: one that failed or found a type error is left
// alone, and so is the load the graph was built from already, which a rename that wrote nothing hands on. The engine
// keeps the graph, which every extra build reads. A package the host restored from export data has no type information
// and is absent from the graph; the standard library's among them are loaded from source onto a graph of their own,
// beside the host's lint (standardLibraryGraph), once a run, since a rename never changes them.
func (engine *pipeline) buildDependenceGraph(loaded *load.Loaded, loadError error) {
	if !engine.request.judgesDependence() || !engine.keepsForBuilds() || !engine.request.requests(contract.PhaseLint) ||
		loadError != nil || loaded == nil || len(loaded.Diagnostics) > 0 ||
		engine.dependence != nil && engine.dependence.from == loaded {
		return
	}
	started := time.Now()
	var every []*packages.Package
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) { every = append(every, each) })
	host := &hostDependence{
		graph:    build_dependence.NewGraph(),
		from:     loaded,
		files:    make(map[string][]string, len(every)),
		paths:    make(map[string]string, len(every)),
		restored: map[string]bool{},
	}
	var restoredStandardLibrary []string
	for _, each := range every {
		host.files[each.ID] = each.GoFiles
		host.paths[each.ID] = each.PkgPath
		if each.TypesInfo != nil {
			continue
		}
		host.restored[each.ID] = true
		// The standard library is the only package in no module that a module's load reaches, and none of it is a
		// test variant here, since only the workspace's packages have tests built.
		if each.Module == nil && each.ForTest == "" {
			restoredStandardLibrary = append(restoredStandardLibrary, each.PkgPath)
		}
	}
	engine.workPool().ForEach(len(every), 0, func(index int) {
		if every[index].TypesInfo != nil {
			host.graph.AddPackage(every[index], engine.described.hasFileInScope(every[index]))
		}
	})
	host.elapsed = time.Since(started)
	switch {
	case engine.dependence != nil && engine.dependence.standardLibrary != nil:
		host.standardLibrary = engine.dependence.standardLibrary
	case len(restoredStandardLibrary) > 0:
		host.standardLibrary = engine.standardLibraryGraph(restoredStandardLibrary)
	}
	engine.dependence = host
}

// standardLibraryGraph starts the standard library's graph (standardLibraryDependence) on a goroutine of its own and
// returns at once: paths, the standard-library packages the host restored, are loaded from source by the engine's own
// loader (loadPackages), as the host's load would have checked them, under the host's environment, on the run's pool,
// without tests, and every package the load reaches is added to the graph, so the graph holds whatever they import too
// and needs no answers of its own. Its work takes the pool's slots beside the host's lint, and the builds wait for it
// (awaitStandardLibrary).
func (engine *pipeline) standardLibraryGraph(paths []string) *standardLibraryDependence {
	side := &standardLibraryDependence{done: make(chan struct{})}
	workspace := engine.described.workspace
	go func() {
		defer close(side.done)
		started := time.Now()
		loaded, loadError := engine.loadPackages(loadRequest{
			environment: workspace.Environment(),
			caching:     &load.Caching{GoEnvironment: engine.described.goEnvironment, WithoutTests: true},
			patterns:    paths,
		})
		side.loadElapsed = time.Since(started)
		switch {
		case loadError != nil:
			side.problem = loadError
			return
		case len(loaded.Diagnostics) > 0:
			side.problem = fmt.Errorf("%d errors, the first %s", len(loaded.Diagnostics), loaded.Diagnostics[0].Message)
			return
		}
		graphStarted := time.Now()
		var every []*packages.Package
		packages.Visit(loaded.Roots, nil, func(each *packages.Package) { every = append(every, each) })
		side.graph = build_dependence.NewGraph()
		side.files = make(map[string][]string, len(every))
		for _, each := range every {
			side.files[each.ID] = each.GoFiles
		}
		side.packages = len(every)
		if len(every) > 0 {
			side.fileSet = every[0].Fset
		}
		engine.workPool().ForEach(len(every), 0, func(index int) {
			side.graph.AddPackage(every[index], false)
		})
		side.graphElapsed = time.Since(graphStarted)
	}()
	return side
}

// awaitStandardLibrary waits for the standard library's graph, if the host has one, and says on stderr when its side
// load failed, so every object of the standard library a unit reads fails wide. otherBuilds calls it once, on the
// pipeline's goroutine, before any extra build starts, so the builds' width is measured with the side load over.
func (engine *pipeline) awaitStandardLibrary() {
	if engine.dependence == nil || engine.dependence.standardLibrary == nil {
		return
	}
	side := engine.dependence.standardLibrary
	<-side.done
	if side.problem != nil {
		engine.note("the standard library's side load for --build-dependence failed, so every object of it a unit "+
			"reads is called dependent: %v", side.problem)
	}
}

// dependenceOf is one extra build's judgment on the host's graph, from its first load, nil when the host built none.
// Each package's files are compared as written (GoFiles), as compiledFiles reads them, since a cgo output's name
// changes from run to run, and the graph names each unit by the file a reader sees. A file only the build compiles is
// parsed for its syntax alone. A package only one side's load reaches is that side's alone, except a root the build's
// load leaves out because its in-package test variant is checked in its place (load.Caching's AnalyzedRootsOnly): the
// variant holds every file of it, so its files are compared with the variant's, and its units are never called
// dependent for a package the build checks by another name. A build loaded again for a question keeps this judgment,
// since its files do not change between rounds. Beside the record it returns the judgment and the files both builds
// compile of each package both have by one ID, which the facts census compares through (compareFacts).
//
// When the host restored packages from export data, which its graph does not hold, the standard library's graph is
// judged first against the build's files of its packages, and that judgment answers for every object the host's units
// read of a restored package (build_dependence.Counterparts), failing wide for one it cannot answer. The record's
// dependencies count the standard library's units beside the host's, so its totals are those of a run whose host
// checked every package from source.
func (engine *pipeline) dependenceOf(loaded *load.Loaded) (*contract.BuildDependence, *buildJudgment) {
	if engine.dependence == nil {
		return nil, nil
	}
	// The builds start once the standard library's graph has ended (awaitStandardLibrary); a build that did not, as a
	// test's might, waits for it here.
	if engine.dependence.standardLibrary != nil {
		<-engine.dependence.standardLibrary.done
	}
	started := time.Now()
	built := map[string]*packages.Package{}
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) { built[each.ID] = each })
	difference := build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	}
	fileSet := token.NewFileSet()
	judged := &buildJudgment{shared: map[sharedFile]bool{}, sharedPackages: map[string]bool{}}
	for identifier, hostFiles := range engine.dependence.files {
		counterpart, isShared := built[identifier]
		asVariant := false
		if !isShared {
			counterpart, isShared = built[identifier+" ["+engine.dependence.paths[identifier]+".test]"]
			asVariant = isShared
		}
		if !isShared {
			difference.HostOnlyPackages[identifier] = true
			continue
		}
		// A package the build restored has its facts' objects from export data, whose positions are no offsets in
		// its files, so the census leaves it out; one the host restored is keyed through its counterpart in the
		// standard library's graph (indexHostFacts).
		if !asVariant && counterpart.TypesInfo != nil {
			judged.sharedPackages[identifier] = true
		}
		// A variant's own test files are the variant's, never the package's it stands in for.
		differing := filesDiffer(hostFiles, counterpart.GoFiles, fileSet, !asVariant)
		if !asVariant {
			for _, file := range hostFiles {
				if slices.Contains(counterpart.GoFiles, file) {
					judged.shared[sharedFile{packageIdentifier: identifier, file: file}] = true
				}
			}
		}
		if len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 {
			difference.Packages[identifier] = differing
		}
	}
	for identifier, each := range built {
		if _, isHosted := engine.dependence.files[identifier]; isHosted {
			continue
		}
		difference.BuildOnlyPackages[identifier] = true
		buildOnly := build_dependence.PackageDifference{Workspace: engine.described.hasFileInScope(each)}
		for _, file := range each.GoFiles {
			buildOnly.BuildOnlyFiles = appendParsed(buildOnly.BuildOnlyFiles, fileSet, file)
		}
		difference.Packages[identifier] = buildOnly
	}
	standardLibrary := engine.dependence.standardLibrary
	var standardLibraryJudged *build_dependence.Dependence
	if standardLibrary != nil && standardLibrary.graph != nil {
		standardLibraryJudged = standardLibrary.graph.Against(
			standardLibraryDifference(standardLibrary, built, fileSet))
	}
	var standardLibraryGraph *build_dependence.Graph
	if standardLibraryJudged != nil {
		standardLibraryGraph = standardLibrary.graph
	}
	counterparts := build_dependence.NewCounterparts(standardLibraryGraph, standardLibraryJudged)
	judged.standardLibrary, judged.counterparts = standardLibraryJudged, counterparts
	// A host that restored nothing holds every package it reached, so nothing is asked, and a graph that asks nothing
	// judges as block 1 always has.
	if len(engine.dependence.restored) > 0 {
		difference.DependentObject = counterparts.DependentObject
	}
	judged.dependence = engine.dependence.graph.Against(difference)
	counts := judged.dependence.Counts()
	record := &contract.BuildDependence{
		GraphMilliseconds:    int(engine.dependence.elapsed.Milliseconds()),
		JudgmentMilliseconds: int(time.Since(started).Milliseconds()),
	}
	if len(engine.dependence.restored) > 0 {
		answered := counterparts.Counts()
		record.Restored = &contract.RestoredDependence{
			Packages:           len(engine.dependence.restored),
			Answered:           answered.Answered,
			WithoutCounterpart: answered.WithoutCounterpart,
			WithoutPackage:     answered.WithoutPackage,
		}
		if standardLibrary != nil {
			record.Restored.StandardLibraryPackages = standardLibrary.packages
			record.Restored.LoadMilliseconds = int(standardLibrary.loadElapsed.Milliseconds())
			record.Restored.GraphMilliseconds = int(standardLibrary.graphElapsed.Milliseconds())
		}
		if standardLibraryJudged != nil {
			standardLibraryCounts := standardLibraryJudged.Counts()
			record.Restored.StandardLibraryUnits = standardLibraryCounts.Dependencies.Units
			record.Restored.StandardLibraryDependentUnits = standardLibraryCounts.Dependencies.DependentUnits
			counts.Workspace.Add(standardLibraryCounts.Workspace)
			counts.Dependencies.Add(standardLibraryCounts.Dependencies)
		}
	}
	record.Workspace = dependenceCounts(counts.Workspace)
	record.Dependencies = dependenceCounts(counts.Dependencies)
	return record, judged
}

// standardLibraryDifference is what the build changes of the standard library's graph, as dependenceOf compares the
// host's packages: each side-loaded package's files with the build's package of that ID, and one the build does not
// have is the host's alone. A package only the build has is no business of this graph's: the host's difference counts
// it, since the host's files name every package the host reached, the standard library's among them.
func standardLibraryDifference(
	standardLibrary *standardLibraryDependence,
	built map[string]*packages.Package,
	fileSet *token.FileSet,
) build_dependence.Difference {
	difference := build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	}
	for identifier, hostFiles := range standardLibrary.files {
		counterpart, isShared := built[identifier]
		if !isShared {
			difference.HostOnlyPackages[identifier] = true
			continue
		}
		differing := filesDiffer(hostFiles, counterpart.GoFiles, fileSet, true)
		if len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 {
			difference.Packages[identifier] = differing
		}
	}
	return difference
}

// filesDiffer is how one package's files differ, as written: hostFiles the host compiles and builtFiles the build
// does, each host-only file named, and with buildOnly each build-only file parsed for its syntax alone.
func filesDiffer(
	hostFiles []string,
	builtFiles []string,
	fileSet *token.FileSet,
	buildOnly bool,
) build_dependence.PackageDifference {
	var differing build_dependence.PackageDifference
	for _, file := range hostFiles {
		if !slices.Contains(builtFiles, file) {
			differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
		}
	}
	if buildOnly {
		for _, file := range builtFiles {
			if !slices.Contains(hostFiles, file) {
				differing.BuildOnlyFiles = appendParsed(differing.BuildOnlyFiles, fileSet, file)
			}
		}
	}
	return differing
}

// appendParsed is files with file's syntax after them, parsed alone, when it parses. A build-only file that does not
// parse fails the build's own load, whose type errors the record already holds, so its units are left uncounted.
func appendParsed(files []*ast.File, fileSet *token.FileSet, file string) []*ast.File {
	parsed, parseError := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
	if parseError != nil || parsed == nil {
		return files
	}
	return append(files, parsed)
}

// dependenceCounts is one scope's counts as the record writes them.
func dependenceCounts(counts build_dependence.ScopeCounts) contract.DependenceCounts {
	return contract.DependenceCounts{
		Units:                 counts.Units,
		DependentUnits:        counts.DependentUnits,
		Nodes:                 counts.Nodes,
		DependentNodes:        counts.DependentNodes,
		Packages:              counts.Packages,
		DependentPackages:     counts.DependentPackages,
		DependentPackageNodes: counts.DependentPackageNodes,
		BuildOnlyUnits:        counts.BuildOnlyUnits,
		BuildOnlyNodes:        counts.BuildOnlyNodes,
	}
}
