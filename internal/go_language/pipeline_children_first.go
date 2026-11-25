package go_language

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_language/project"
)

// startChildrenFirst has every go command a run with extra builds needs started before the host's first load, and
// waits for them (#sktb2y8): the host's lister state, and each extra build's go env and lister state. On darwin go
// starts a child process with fork, which marks the whole address space copy-on-write, so each child started once the
// heap has grown makes every resident page fault again on its next write, and the builds' commands, started as each
// build loads, started with cohere-go at gigabytes resident. Started early they are the same commands under the same
// environments and guards, so every answer is the one the load would have read: each build's go env is kept for its
// run (extraBuildRun), and each answer's lister state is fetched once for every load under it
// (load.GoEnvironment.FetchListerState). A build whose `go list -m -json all` prints what the host's does reads the
// host's, its failure too, in place of running its own, and a build context the system cache keeps from an earlier run
// is read from there, outside `--timing` (load.GoEnvironment.ShareModulesOf, load.GoEnvironment.KeepContext,
// #sfjj2rh).
//
// describeProject starts them beside its census and its reads, once it has read the builds, and the host's lister
// state in any run that loads (earlyStart, #q9ezwvw), so here the run waits for whatever of them is left: each to its
// end, but a build predicted to fork nothing after its go env, whose wait ends once its go env has started, its go env
// running on beside the host's listing, and its run waiting for it where it first reads it (buildStarts). Under
// `--timing` they start here, one at a time, as the builds run, so each measurement of their children's CPU is of its
// own commands alone, and a run of one build fetches its lister state within its first load.
func (pipeline *pipeline) startChildrenFirst() {
	// Whatever describeProject started that the first load waits for has ended first, and an extra build predicted to
	// fork nothing after its go env has started that, so none of it starts a child after the first load begins unless
	// its own answer contradicts the prediction (buildStarts).
	pipeline.described.awaitForked()
	// A run that checks no extra build, or whose builds could not be read, starts nothing here.
	_, extras, buildsError := pipeline.buildsBeside()
	if !pipeline.keepsForBuilds() || buildsError != nil {
		return
	}
	var builds []*buildGoEnvironment
	if pipeline.described.started != nil && pipeline.described.started.buildGoEnvironments != nil {
		builds = pipeline.described.started.buildGoEnvironments
		// Fetched beside the reads already, so this reads what they printed and starts nothing; it runs the two only
		// when the early start did not.
		if pipeline.described.goEnvironment != nil {
			pipeline.described.goEnvironment.FetchListerState(pipeline.timing)
		}
	} else {
		builds = pipeline.startChildrenNow(extras)
	}
	pipeline.buildGoEnvironments = map[string]*buildGoEnvironment{}
	for index, extra := range extras {
		pipeline.buildGoEnvironments[extra.String()] = builds[index]
	}
}

// startChildrenNow starts the host's lister state, and each of extras' go env and lister state (buildStarts), at once,
// or one at a time under `--timing`, and returns each extra build's go env by its index, once the first load may
// begin: the path of a run whose description started none of them (startChildrenFirst). A build that reads the host's
// build list waits for it, or runs it, under the host's environment and guard, whichever step comes first.
func (pipeline *pipeline) startChildrenNow(extras []project.Build) []*buildGoEnvironment {
	var oneAtATime sync.Mutex
	var awaited sync.WaitGroup
	// The host's answer is nil only for a census that judged no file, which reads no builds.
	if pipeline.described.goEnvironment != nil {
		awaited.Go(func() {
			pipeline.childrenFirstStep(&oneAtATime, func() {
				pipeline.described.goEnvironment.FetchListerState(pipeline.timing)
			})
		})
	}
	// Every build here is waited out whole, joined and awaited by the one group: a run reaches startChildrenNow only
	// under `--timing`, where nothing is predicted (the description starts the builds' commands in any other run that
	// reads builds, startReadingBuilds), so no build's go env runs on past this wait and nothing is left to join later.
	starts := &buildStarts{workspace: pipeline.described.workspace, host: pipeline.described.goEnvironment,
		children: pipeline.described.children, contexts: pipeline.systemCache.contexts(), collector: pipeline.timing,
		joined: &awaited, awaited: &awaited}
	// Under `--timing` each build starts one at a time, and none predicts or keeps anything, so each step's children's
	// CPU is of its own commands.
	if pipeline.timing != nil {
		starts.oneAtATime = &oneAtATime
	}
	builds := make([]*buildGoEnvironment, len(extras))
	for index, extra := range extras {
		builds[index] = starts.start(extra)
	}
	awaited.Wait()
	return builds
}

// joinChildren joins every go command the run started before its first load, each to its end: those describeProject
// started (describedProject.awaitStarted), so none outlives the run, whichever way it ends. startChildrenNow's own
// have all ended before it returns.
func (pipeline *pipeline) joinChildren() {
	if pipeline.watchJoin != nil {
		pipeline.watchJoin()
	}
	pipeline.described.awaitStarted()
}

// childrenFirstStep runs step, one of startChildrenNow's, under oneAtATime in a run under `--timing`, and at once
// with the others in any other.
func (pipeline *pipeline) childrenFirstStep(oneAtATime *sync.Mutex, step func()) {
	if pipeline.timing != nil {
		oneAtATime.Lock()
		defer oneAtATime.Unlock()
	}
	step()
}
