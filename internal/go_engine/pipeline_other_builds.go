package go_engine

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// otherBuilds is the run under every build beside the host's that the census needs (#72n9mq0), each extra build
// checking the whole workspace as it builds there: a run per build, the host's first, from loaded and pass, the host's
// load and the lint pass it wrote. It is nil when no extra build is needed, so such a run checks nothing more, and
// when the builds could not be read, which it notes, and the run is incomplete.
//
// The host's run is its lint pass as it is. Each extra build loads the workspace under its own environment and runs
// the enabled rules over every package, through its own findings cache (extraBuildRun). A file no build the
// toolchain targets compiles (project.Unbuilt) stays the gap the types phase names.
func (engine *pipeline) otherBuilds(loaded *load.Loaded, pass *lintPass) []buildRun {
	host, extras, buildsError := engine.buildsBeside()
	if buildsError != nil {
		engine.otherBuildsFailed(buildsError)
		return nil
	}
	if len(extras) == 0 {
		return nil
	}
	ours := engine.oursSet()
	analyzed := map[string]bool{}
	for _, each := range loaded.Packages {
		analyzed[each.ID] = true
	}
	runs := []buildRun{{
		build:      host,
		compiled:   compiledFiles(loaded.Roots, ours),
		analyzed:   analyzed,
		said:       compiledFiles(loaded.Packages, ours),
		result:     pass.result,
		typeErrors: loaded.Diagnostics,
		inputs:     loaded.Inputs,
	}}

	for _, extra := range extras {
		extraRun, extraError := engine.extraBuildRun(extra, ours)
		if extraError != nil {
			engine.otherBuildsFailed(extraError)
			return nil
		}
		runs = append(runs, extraRun)
	}
	return runs
}

// buildsBeside is the host's build, as the load's go env answer reads it, and the extra builds the census needs
// beside it, on the host's `GOARCH` (project.BuildsFor), with the targets the toolchain lists. A census that judged no
// file has no go env answer, and needs no extra build.
func (engine *pipeline) buildsBeside() (project.Build, []project.Build, error) {
	if engine.described.goEnvironment == nil {
		return project.Build{}, nil, nil
	}
	buildContext, goFlags := engine.described.goEnvironment.Target()
	host := project.HostBuild(buildContext, goFlags)
	ports, portsError := project.ReadPorts(engine.described.workspace.Root, engine.described.workspace.Environment())
	if portsError != nil {
		return project.Build{}, nil, portsError
	}
	// Each unbuilt file is left to the types phase, which names it as a gap.
	builds, _, buildsError := project.BuildsFor(engine.described.census, host, ports)
	if buildsError != nil {
		return project.Build{}, nil, buildsError
	}
	return host, builds[1:], nil
}

// extraBuildRun is one extra build's run: the workspace loaded under its environment, as the rename's platformLoad
// loads a platform, and the run's enabled rules over every package it loaded, as the host's lint runs, through a
// findings cache of the build's own, whose keys hold the build's target, cgo and flags as go env reads them under its
// environment, so they never meet the host's. Its type errors are its load's for every package. A load with any
// analyzes nothing, as the host's lint runs over no load with one, since findings against wrong semantics would be
// noise, and so has a say on no file.
func (engine *pipeline) extraBuildRun(extra project.Build, ours map[string]bool) (buildRun, error) {
	environment := append(slices.Clone(engine.described.workspace.Environment()), extra.Environment()...)
	var session *check.CacheSession
	// As the host's lint does, a run that renamed reads no findings cache, whose keys are of the bytes it replaced.
	if !engine.renamed {
		session = engine.cacheSession(environment)
	}
	caching := load.Caching{Held: engine.held}
	if session != nil {
		caching.Restorer = session
		if session.HasStore() {
			caching.Listings = session
		}
	}
	loaded, loadError := load.PackagesCached(engine.described.workspace.Root, engine.described.workspace.Patterns(),
		environment, engine.described.readableOurs, caching)
	if loadError != nil {
		return buildRun{}, fmt.Errorf("loading the build %s: %w", extra, loadError)
	}
	run := buildRun{
		build:      extra,
		compiled:   compiledFiles(loaded.Roots, ours),
		analyzed:   map[string]bool{},
		said:       map[string]bool{},
		result:     &check.Result{FindingsByRule: map[string]int{}},
		typeErrors: loaded.Diagnostics,
		inputs:     loaded.Inputs,
	}
	if len(loaded.Diagnostics) > 0 {
		return run, nil
	}
	for _, each := range loaded.Packages {
		run.analyzed[each.ID] = true
	}
	run.said = compiledFiles(loaded.Packages, ours)
	enabled, _ := engine.enabledRules()
	result, checkError := check.Run(loaded.Packages, enabled, check.Options{
		SingleThreaded: engine.request.switches["single-threaded"],
		Cache:          session,
		Withheld:       loaded.Withheld,
		// Nothing reads the build's syntax after its rules: the merge reads only what the run hands it.
		ReleaseAnalyzed: true,
		ReadFile:        engine.sourceReader(nil),
	})
	if checkError != nil {
		return buildRun{}, fmt.Errorf("running the rules over the build %s: %w", extra, checkError)
	}
	for _, problem := range result.Cache.Problems {
		engine.note("the findings cache of the build %s: %s", extra, problem)
	}
	run.result = result
	return run, nil
}

// otherBuildsFailed notes why the builds beside the host's were not checked, which leaves the run incomplete: it
// was asked for them, and checked only the host's.
func (engine *pipeline) otherBuildsFailed(cause error) {
	engine.note("the builds beside the host's were not checked: %v", cause)
	engine.incomplete = true
}

// oursSet is every readable file of ours, as the census and the load account for them.
func (engine *pipeline) oursSet() map[string]bool {
	ours := make(map[string]bool, len(engine.described.readableOurs))
	for _, file := range engine.described.readableOurs {
		ours[file] = true
	}
	return ours
}

// compiledFiles is every file of ours the packages compile, by path: their GoFiles, as written, rather than their
// CompiledGoFiles, whose cgo outputs are named per run.
func compiledFiles(compiled []*packages.Package, ours map[string]bool) map[string]bool {
	files := map[string]bool{}
	for _, each := range compiled {
		for _, file := range each.GoFiles {
			if ours[file] {
				files[file] = true
			}
		}
	}
	return files
}

// mergeBuildRuns is where every build's run meets, the host's first (#72n9mq0): each build's findings in scope after
// its own //cohere:ignore comments (outcomeOf), merged into the one verdict the lint phase writes (mergeBuilds), and
// what each build covered and brought, for the lint record. ran are the analyzer rules that ran, the same in every
// build. It keeps the runs too, so a test reads what each build analyzed.
func (engine *pipeline) mergeBuildRuns(runs []buildRun, ran []string) *buildsVerdict {
	engine.buildRuns = runs
	outcomes := make([]buildOutcome, 0, len(runs))
	for _, run := range runs {
		outcomes = append(outcomes, engine.outcomeOf(run, ran))
	}
	verdict := &buildsVerdict{merged: mergeBuilds(outcomes, mergeIfAllRules(engine.registered))}
	for index, run := range runs {
		record := contract.BuildRecord{
			Target:           run.build.String(),
			Host:             index == 0,
			CompiledFiles:    len(run.compiled),
			AnalyzedPackages: len(run.analyzed),
			FindingsAdded:    verdict.merged.added[index],
			FindingsDropped:  verdict.merged.dropped[index],
		}
		verdict.records = append(verdict.records, record)
	}
	return verdict
}
