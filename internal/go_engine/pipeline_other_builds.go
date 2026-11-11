package go_engine

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
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
	if engine.session != nil {
		runs[0].files = engine.session.Files()
	}

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

// buildsBeside is the host's build and the extra builds the census needs beside it, as describeProject read them
// (readBuilds), so the builds the lint phase checks are the ones the project record names a file checked by. A census
// that judged no file read none, and needs no extra build.
func (engine *pipeline) buildsBeside() (project.Build, []project.Build, error) {
	if engine.described.buildsError != nil {
		return project.Build{}, nil, engine.described.buildsError
	}
	if len(engine.described.builds) == 0 {
		return project.Build{}, nil, nil
	}
	return engine.described.builds[0], engine.described.builds[1:], nil
}

// readBuilds is the host's build, as go env's answer reads it, then the extra builds the census needs beside it
// (project.BuildsFor), with the targets the toolchain lists, and why each file no build compiles is left so, by path.
// targets are the settings' `targets`, each `GOOS/GOARCH` a port the toolchain lists or the run is refused: a
// target named and never checked would read as checked (#ntamp9e). Each file no build compiles is left to the types
// phase, which names it as a gap with that reason.
func readBuilds(
	workspace project.Workspace,
	census project.Census,
	goEnvironment *load.GoEnvironment,
	targets []string,
) ([]project.Build, map[string]string, error) {
	buildContext, goFlags := goEnvironment.Target()
	host := project.HostBuild(buildContext, goFlags)
	ports, portsError := project.ReadPorts(workspace.Root, workspace.Environment())
	if portsError != nil {
		return nil, nil, portsError
	}
	var extraTargets []project.Port
	for _, target := range targets {
		operatingSystem, architecture, _ := strings.Cut(target, "/")
		index := slices.IndexFunc(ports, func(port project.Port) bool {
			return port.OperatingSystem == operatingSystem && port.Architecture == architecture
		})
		if index < 0 {
			return nil, nil, refusedTarget{fmt.Errorf("the go block's targets name %s, which go tool dist list does "+
				"not target, so it could never be checked", target)}
		}
		extraTargets = append(extraTargets, ports[index])
	}
	builds, unbuilt, buildsError := project.BuildsFor(census, host, ports, extraTargets)
	if buildsError != nil {
		return nil, nil, buildsError
	}
	unbuiltReasons := make(map[string]string, len(unbuilt))
	for _, each := range unbuilt {
		unbuiltReasons[each.File] = each.Reason
	}
	return builds, unbuiltReasons, nil
}

// refusedTarget is a target the settings name that no port of the toolchain is, which refuses the run before its
// first record rather than leaving it incomplete, since the settings asked for a check that cannot be made.
type refusedTarget struct{ error }

// checkingBuild is the first extra build of builds, the host's first, that compiles file, read from contents, which
// the project record names it checked by. It reports false when none does: no build was read, or the file is one
// BuildsFor leaves unbuilt, which keeps its constraint reason and stays a gap.
func checkingBuild(builds []project.Build, file string, contents []byte) (project.Build, bool, error) {
	if len(builds) < 2 {
		return project.Build{}, false, nil
	}
	for _, extra := range builds[1:] {
		compiled, compileError := extra.Compiles(file, contents)
		if compileError != nil {
			return project.Build{}, false, compileError
		}
		if compiled {
			return extra, true, nil
		}
	}
	return project.Build{}, false, nil
}

// extraBuildRun is one extra build's run: the workspace loaded under its environment, as the rename's platformLoad
// loads a platform, and the run's enabled rules over every package it loaded, as the host's lint runs, through a
// findings cache of the build's own, whose keys hold the build's target, cgo and flags as go env reads them under its
// environment, so they never meet the host's. Its type errors are its load's for every package. A load with any
// analyzes nothing, as the host's lint runs over no load with one, since findings against wrong semantics would be
// noise, and so has a say on no file.
//
// The build's keys read the digests of the files the host's session hashed (CacheSession.Files) rather than hash them
// again, since nearly every file is the host's too. That holds because nothing writes a file between the host's load
// and this one: gofmt, the long-line wrap and the rename all write before the load the lint reads (pipeline.run), the
// wrap's write is followed by a second load whose Restore keys every file again, and a run whose rename wrote reads no
// findings cache here at all.
func (engine *pipeline) extraBuildRun(extra project.Build, ours map[string]bool) (buildRun, error) {
	environment := append(slices.Clone(engine.described.workspace.Environment()), extra.Environment()...)
	var session *check.CacheSession
	// As the host's lint does, a run that renamed reads no findings cache, whose keys are of the bytes it replaced.
	if !engine.renamed {
		var hostFiles cache.FileDigests
		if engine.session != nil {
			hostFiles = engine.session.Files()
		}
		session = engine.cacheSession(environment, hostFiles)
	}
	// As the host's load does (loadWorkspace), a package with no file in scope is loaded and never analyzed.
	caching := load.Caching{Held: engine.held, Timing: engine.timing, Analyzes: engine.described.hasFileInScope}
	if session != nil {
		caching.Restorer = session
		if session.HasStore() {
			caching.Listings = session
		}
	}
	loaded, loadError := engine.loadPackages(loadRequest{environment: environment, caching: &caching})
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
	if session != nil {
		run.files = session.Files()
	}
	if len(loaded.Diagnostics) > 0 {
		return run, nil
	}
	for _, each := range loaded.Packages {
		run.analyzed[each.ID] = true
	}
	run.said = compiledFiles(loaded.Packages, ours)
	enabled, _ := engine.enabledRules()
	runRules := func(over *load.Loaded) (*check.Result, error) {
		return check.Run(over.Packages, enabled, check.Options{
			SingleThreaded: engine.request.switches["single-threaded"],
			Cache:          session,
			Withheld:       over.Withheld,
			// Nothing reads the build's syntax after its rules: the merge reads only what the run hands it.
			ReleaseAnalyzed: true,
			ReadFile:        engine.sourceReader(nil, over.Rewritten),
			Timing:          engine.timing,
		})
	}
	result, checkError := runRules(loaded)
	// As the host's lint does (reloadedPass), a question that reached a package the cache restored, with no answer
	// stored, has the build loaded again with that package checked from source, until none does.
	for checkError == nil && len(result.Reloads) > 0 && session != nil && session.Reload(result.Reloads) {
		reloads := strings.Join(result.Reloads, ", ")
		loaded, loadError = engine.loadPackages(loadRequest{environment: environment, caching: &caching})
		if loadError != nil {
			return buildRun{}, fmt.Errorf("loading the build %s again to check %s from source: %w", extra, reloads,
				loadError)
		}
		if len(loaded.Diagnostics) > 0 {
			return buildRun{}, fmt.Errorf("loading the build %s again to check %s from source found %d type "+
				"errors the first load did not", extra, reloads, len(loaded.Diagnostics))
		}
		result, checkError = runRules(loaded)
	}
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
	engine.recordBuildCoverage(runs, verdict.records)
	return verdict
}

// recordBuildCoverage counts, into each build's record, what it alone covered and what it compiled that no rule ran
// on (#ytnvt8k). A file of ours in scope that only one build compiles is that build's alone, counted when a rule ran
// on it. A file the host's build leaves out that no build analyzed was compiled and never linted, as a build whose
// load found a type error analyzes no package, so it is named under the first build that compiles it, with why,
// and the front door names it as not checked rather than as checked by that build.
//
// Mutates: records gain each build's FilesAlone, NotLinted and NotLintedReason
func (engine *pipeline) recordBuildCoverage(runs []buildRun, records []contract.BuildRecord) {
	inScope := func(file string) bool {
		return engine.described.inScopeSet[file] || engine.described.constrainedInScope[file]
	}
	compiledBy := map[string][]int{}
	for index, run := range runs {
		for file := range run.compiled {
			if inScope(file) {
				compiledBy[file] = append(compiledBy[file], index)
			}
		}
	}
	for file, builds := range compiledBy {
		slices.Sort(builds)
		linted := slices.ContainsFunc(builds, func(index int) bool { return runs[index].said[file] })
		if len(builds) == 1 && linted {
			records[builds[0]].FilesAlone++
		}
		if !linted && engine.described.constrainedInScope[file] {
			records[builds[0]].NotLinted = append(records[builds[0]].NotLinted, file)
		}
	}
	for index := range records {
		if len(records[index].NotLinted) == 0 {
			continue
		}
		slices.Sort(records[index].NotLinted)
		reason := "no build that compiles it analyzed its package"
		if len(runs[index].analyzed) == 0 && len(runs[index].typeErrors) > 0 {
			found := "a type error"
			if len(runs[index].typeErrors) > 1 {
				found = fmt.Sprintf("%d type errors", len(runs[index].typeErrors))
			}
			reason = "the " + runs[index].build.String() + " build compiles it, and its load found " + found +
				", so it analyzed no package"
		}
		records[index].NotLintedReason = reason
	}
}
