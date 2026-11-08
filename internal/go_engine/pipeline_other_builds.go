package go_engine

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/platform"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// otherBuilds is the run under every build beside the host's that the census needs (#72n9mq0), each extra build
// checking only what can differ from the host's: a run per build, the host's first, from loaded and pass, the host's
// load and the lint pass it wrote. It is nil when no extra build is needed, so such a run checks nothing more, and
// when the builds could not be read, which it notes, and the run is incomplete.
//
// The host's run is its lint pass as it is. Each extra build loads the workspace under its own environment, and
// check.ReadBuild reads it and a second load of the host's from source, since the host's lint let its packages' syntax
// go and a load the findings cache restored has none; platform.Closure compares the two, and the run's enabled rules
// run over the closure's members, with no findings cache, whose keys are the host's build's alone. A file no build
// the toolchain targets compiles (project.Unbuilt) stays the gap the types phase names.
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
	}}

	hostLoad, hostLoadError := engine.hostBuildLoad(host)
	if hostLoadError != nil {
		engine.otherBuildsFailed(hostLoadError)
		return nil
	}
	for _, extra := range extras {
		extraRun, extraError := engine.extraBuildRun(hostLoad, extra, ours)
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

// hostBuildLoad is the host's build loaded again and read (check.ReadBuild), for the closure to compare each extra
// build with: from source, under the environment and go env answer the host's lint load had, so its packages are
// the ones that lint ran over, each with its syntax and types.
func (engine *pipeline) hostBuildLoad(host project.Build) (platform.BuildLoad, error) {
	loaded, loadError := load.PackagesCached(engine.described.workspace.Root, engine.described.workspace.Patterns(),
		engine.described.workspace.Environment(), engine.described.readableOurs,
		load.Caching{Held: engine.held, GoEnvironment: engine.described.goEnvironment})
	if loadError != nil {
		return platform.BuildLoad{}, fmt.Errorf("loading the host's build %s again from source: %w", host, loadError)
	}
	return engine.readBuildLoad(host, loaded)
}

// readBuildLoad is loaded, a load under target, as the closure reads it: its packages and check.ReadBuild's digests
// of them, read with the run's enabled rules.
func (engine *pipeline) readBuildLoad(target project.Build, loaded *load.Loaded) (platform.BuildLoad, error) {
	enabled, _ := engine.enabledRules()
	objects, ours, readError := check.ReadBuild(loaded, enabled)
	if readError != nil {
		return platform.BuildLoad{}, fmt.Errorf("reading the build %s: %w", target, readError)
	}
	return platform.BuildLoad{
		Build:          target,
		Packages:       loaded.Packages,
		ObjectDigests:  objects,
		PackageDigests: ours,
	}, nil
}

// extraBuildRun is one extra build's run: the workspace loaded under its environment, as the rename's platformLoad
// loads a platform, and the run's enabled rules over the closure of what it can find that the host's run cannot.
// Its type errors are its load's for every package, analyzed or not. A load with any analyzes nothing, as the host's
// lint runs over no load with one, since findings against wrong semantics would be noise, and so has a say on no
// file.
func (engine *pipeline) extraBuildRun(
	hostLoad platform.BuildLoad, extra project.Build, ours map[string]bool,
) (buildRun, error) {
	environment := append(slices.Clone(engine.described.workspace.Environment()), extra.Environment()...)
	loaded, loadError := load.PackagesCached(engine.described.workspace.Root, engine.described.workspace.Patterns(),
		environment, engine.described.readableOurs, load.Caching{Held: engine.held})
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
	}
	if len(loaded.Diagnostics) > 0 {
		return run, nil
	}
	extraLoad, readError := engine.readBuildLoad(extra, loaded)
	if readError != nil {
		return buildRun{}, readError
	}
	members, closureError := platform.Closure(hostLoad, extraLoad)
	if closureError != nil {
		return buildRun{}, fmt.Errorf("the closure of the build %s: %w", extra, closureError)
	}
	for _, member := range members {
		run.analyzed[member.Id] = true
	}
	var closure []*packages.Package
	for _, each := range loaded.Packages {
		if run.analyzed[each.ID] {
			closure = append(closure, each)
		}
	}
	run.said = compiledFiles(closure, ours)
	enabled, _ := engine.enabledRules()
	result, checkError := check.Run(closure, enabled, check.Options{
		SingleThreaded: engine.request.switches["single-threaded"],
		Withheld:       loaded.Withheld,
		// Nothing reads the closure's syntax after its rules: what the run hands the merge was read above.
		ReleaseAnalyzed: true,
		ReadFile:        engine.sourceReader(nil),
	})
	if checkError != nil {
		return buildRun{}, fmt.Errorf("running the rules over the build %s's closure: %w", extra, checkError)
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

// mergeBuildRuns is where every build's run meets, the host's first. Front door wires the merge of their findings
// across builds here (#72n9mq0): until then it writes nothing, and keeps the runs, so a test counts them and reads
// what each build analyzed.
func (engine *pipeline) mergeBuildRuns(runs []buildRun) {
	engine.buildRuns = runs
}
