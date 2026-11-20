package go_engine

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// startChildrenFirst has every go command a run with extra builds needs started before the host's first load, and
// waits for them (#sktb2y8): the host's lister state, and each extra build's go env and lister state. On darwin go
// starts a child process with fork, which marks the whole address space copy-on-write, so each child started once the
// heap has grown makes every resident page fault again on its next write, and the builds' commands, started as each
// build loads, started with the engine at gigabytes resident. Started early they are the same commands under the same
// environments and guards, so every answer is the one the load would have read: each build's go env is kept for its
// run (extraBuildRun), and each answer's lister state is fetched once for every load under it
// (load.GoEnvironment.FetchListerState). A build whose `go list -m -json all` prints what the host's does reads the
// host's, its failure too, in place of running its own, and a build context the system cache keeps from an earlier run
// is read from there, outside `--timing` (load.GoEnvironment.ShareModulesOf, load.GoEnvironment.KeepContextIn,
// #sfjj2rh).
//
// describeProject starts them beside its census and its reads, once it has read the builds, and the host's lister
// state in any run that loads (earlyStart, #q9ezwvw), so here the run waits for whatever of them is left. Under
// `--timing` they start here, one at a time, as the builds run, so each measurement of their children's CPU is of its
// own commands alone, and a run of one build fetches its lister state within its first load.
func (engine *pipeline) startChildrenFirst() {
	// Whatever describeProject started ends first, so none of it starts or runs on after the first load begins.
	engine.described.awaitStarted()
	// A run that checks no extra build, or whose builds could not be read, starts nothing here.
	_, extras, buildsError := engine.buildsBeside()
	if !engine.keepsForBuilds() || buildsError != nil {
		return
	}
	var goEnvironments []*load.GoEnvironment
	if engine.described.started != nil && engine.described.started.buildGoEnvironments != nil {
		goEnvironments = engine.described.started.buildGoEnvironments
		// Fetched beside the reads already, so this reads what they printed and starts nothing; it runs the two only
		// when the early start did not.
		if engine.described.goEnvironment != nil {
			engine.described.goEnvironment.FetchListerState(engine.timing)
		}
	} else {
		goEnvironments = engine.startChildrenNow(extras)
	}
	engine.buildGoEnvironments = map[string]*load.GoEnvironment{}
	for index, extra := range extras {
		if goEnvironments[index] != nil {
			engine.buildGoEnvironments[extra.String()] = goEnvironments[index]
		}
	}
}

// startChildrenNow starts the host's lister state, and each of extras' go env and lister state, at once, or one at a
// time under `--timing`, and returns each extra build's go env by its index, nil where go env failed: the path of a run
// whose description started none of them (startChildrenFirst). A build that reads the host's build list waits for it,
// or runs it, under the host's environment and guard, whichever step comes first.
func (engine *pipeline) startChildrenNow(extras []project.Build) []*load.GoEnvironment {
	goEnvironments := make([]*load.GoEnvironment, len(extras))
	var oneAtATime sync.Mutex
	var group sync.WaitGroup
	// The host's answer is nil only for a census that judged no file, which reads no builds.
	if engine.described.goEnvironment != nil {
		group.Go(func() {
			engine.childrenFirstStep(&oneAtATime, func() {
				engine.described.goEnvironment.FetchListerState(engine.timing)
			})
		})
	}
	for index, extra := range extras {
		group.Go(func() {
			engine.childrenFirstStep(&oneAtATime, func() {
				// Ignored: when go env fails the build reads it again as it runs, as it did before, and fails with go
				// env's own words.
				goEnvironment, _ := load.ReadGoEnvironment(engine.described.workspace.Root,
					extraBuildEnvironment(engine.described.workspace, extra), nil, engine.described.children)
				if goEnvironment != nil {
					// Under `--timing` each build lists its own, so each step's children's CPU is of its own commands.
					goEnvironment.ShareModulesOf(engine.described.goEnvironment, engine.timing != nil)
					goEnvironment.KeepContextIn(engine.systemCache.contexts(), engine.described.goEnvironment,
						engine.timing != nil)
					goEnvironment.FetchListerState(engine.timing)
				}
				goEnvironments[index] = goEnvironment
			})
		})
	}
	group.Wait()
	return goEnvironments
}

// childrenFirstStep runs step, one of startChildrenNow's, under oneAtATime in a run under `--timing`, and at once
// with the others in any other.
func (engine *pipeline) childrenFirstStep(oneAtATime *sync.Mutex, step func()) {
	if engine.timing != nil {
		oneAtATime.Lock()
		defer oneAtATime.Unlock()
	}
	step()
}
