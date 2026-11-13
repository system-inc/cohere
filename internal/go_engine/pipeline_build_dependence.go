package go_engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// hostDependence is the host's graph of its package-level declarations under `--build-dependence` (#qkq4gx5), built
// once from its load for every extra build to be judged on, and what each judgment compares a build's load with: the
// host's files of every package as written, and its path, by package ID, the load it was built from, and how long it
// took.
type hostDependence struct {
	graph *build_dependence.Graph
	from  *load.Loaded
	// facts are the host's facts by object and kind (indexHostFacts), indexed once its lint has run, before any extra
	// build starts, nil until then.
	facts   map[factKey]check.ExportedFact
	files   map[string][]string
	paths   map[string]string
	elapsed time.Duration
}

// buildDependenceGraph builds the host's graph from loaded, every package it checked from source, on the run's pool,
// while their syntax and type information are whole: the rules let go of each package's as they finish with it, so
// this runs between the load's end and the first rules' start, the rename's pass's or the lint phase's, which a run
// judging dependence never overlaps with its load (lintOverlaps). A package with a file in scope is the workspace's,
// its test variants included, and every other a dependency. It builds nothing unless the run judges dependence and the
// lint phase has extra builds to judge, over a load that will be linted: one that failed or found a type error is left
// alone, and so is the load the graph was built from already, which a rename that wrote nothing hands on. The engine
// keeps the graph, which every extra build reads.
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
		graph: build_dependence.NewGraph(),
		from:  loaded,
		files: make(map[string][]string, len(every)),
		paths: make(map[string]string, len(every)),
	}
	for _, each := range every {
		host.files[each.ID] = each.GoFiles
		host.paths[each.ID] = each.PkgPath
	}
	engine.workPool().ForEach(len(every), 0, func(index int) {
		if every[index].TypesInfo != nil {
			host.graph.AddPackage(every[index], engine.described.hasFileInScope(every[index]))
		}
	})
	host.elapsed = time.Since(started)
	engine.dependence = host
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
func (engine *pipeline) dependenceOf(loaded *load.Loaded) (*contract.BuildDependence, *buildJudgment) {
	if engine.dependence == nil {
		return nil, nil
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
		if !asVariant {
			judged.sharedPackages[identifier] = true
		}
		var differing build_dependence.PackageDifference
		for _, file := range hostFiles {
			switch {
			case !slices.Contains(counterpart.GoFiles, file):
				differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
			case !asVariant:
				judged.shared[sharedFile{packageIdentifier: identifier, file: file}] = true
			}
		}
		// A variant's own test files are the variant's, never the package's it stands in for.
		if !asVariant {
			for _, file := range counterpart.GoFiles {
				if !slices.Contains(hostFiles, file) {
					differing.BuildOnlyFiles = appendParsed(differing.BuildOnlyFiles, fileSet, file)
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
	judged.dependence = engine.dependence.graph.Against(difference)
	counts := judged.dependence.Counts()
	return &contract.BuildDependence{
		Workspace:            dependenceCounts(counts.Workspace),
		Dependencies:         dependenceCounts(counts.Dependencies),
		GraphMilliseconds:    int(engine.dependence.elapsed.Milliseconds()),
		JudgmentMilliseconds: int(time.Since(started).Milliseconds()),
	}, judged
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
