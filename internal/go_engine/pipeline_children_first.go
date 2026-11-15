package go_engine

import (
	"slices"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// startChildrenFirst starts every go command a run with extra builds needs, all at once, before the host's first
// load, and waits for them (#sktb2y8): the host's lister state, and each extra build's go env and lister state. On
// darwin go starts a child process with fork, which marks the whole address space copy-on-write, so each child started
// once the heap has grown makes every resident page fault again on its next write, and the builds' commands, started
// as each build loads, started with the engine at gigabytes resident. Started here they are the same commands under
// the same environments and guards, only earlier, so every answer is the one the load would have read: each build's go
// env is kept for its run (extraBuildRun), and each answer's lister state is fetched once for every load under it
// (load.GoEnvironment.FetchListerState). Each build keeps its own `go list -m -json all`, never another's. A run that
// checks no extra build starts nothing here: its host's lister state is the first load's own.
//
// Under `--timing` they run one at a time, as the builds do, so each measurement of their children's CPU is of its own
// commands alone.
func (engine *pipeline) startChildrenFirst() {
	// A run that checks no extra build, or whose builds could not be read, starts nothing here.
	_, extras, buildsError := engine.buildsBeside()
	if !engine.keepsForBuilds() || buildsError != nil {
		return
	}
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
					engine.extraBuildEnvironment(extra), nil, engine.described.children)
				if goEnvironment != nil {
					goEnvironment.FetchListerState(engine.timing)
				}
				goEnvironments[index] = goEnvironment
			})
		})
	}
	group.Wait()
	engine.buildGoEnvironments = map[string]*load.GoEnvironment{}
	for index, extra := range extras {
		if goEnvironments[index] != nil {
			engine.buildGoEnvironments[extra.String()] = goEnvironments[index]
		}
	}
}

// childrenFirstStep runs step, one of startChildrenFirst's, under oneAtATime in a run under `--timing`, and at once
// with the others in any other.
func (engine *pipeline) childrenFirstStep(oneAtATime *sync.Mutex, step func()) {
	if engine.timing != nil {
		oneAtATime.Lock()
		defer oneAtATime.Unlock()
	}
	step()
}

// extraBuildEnvironment is the environment an extra build's loads run under: the workspace's, then the build's target,
// cgo and flags.
func (engine *pipeline) extraBuildEnvironment(extra project.Build) []string {
	return append(slices.Clone(engine.described.workspace.Environment()), extra.Environment()...)
}
