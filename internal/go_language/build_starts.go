package go_language

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/go_language/timing"
)

// buildStarts starts extra builds' go env and lister state before the run's first load, each build on a goroutine of
// its own (start): the early start's batch (earlyStart.startBuilds), and the run's when the description started none
// (startChildrenNow).
type buildStarts struct {
	workspace project.Workspace

	// host is the host's go env, which each build's predictions and checks compare with.
	host *load.GoEnvironment

	// children is the run's account of the go commands it starts.
	children *load.Children

	// contexts is where each build context is kept across runs, the system cache's store, nil for none.
	contexts load.ListingStore

	// collector is the run's timing collector, under `--timing` alone, where nothing is predicted or kept, and
	// oneAtATime, there, has each build start under it, one at a time, so each step's children's CPU is of its own
	// commands; both nil in any other run.
	collector  *timing.Collector
	oneAtATime *sync.Mutex

	// joined counts every build's goroutine until it ends, which the run joins before it returns, and awaited counts
	// what the run's first load waits for: each build until its goroutine ends, but a build predicted to fork nothing
	// after its go env, only until its go env has started.
	joined  *sync.WaitGroup
	awaited *sync.WaitGroup
}

// start starts extra's go env in the workspace under the build's environment, then its lister state, on a goroutine
// joined counts, every go command recorded in children, and returns the build's go env to read once it is (#sfjj2rh).
// Before go env starts it decides, from the host's go env and the build's own entries alone, whether the build forks
// nothing after: its build list predicted the host's (load.ModulesPredictedShared) and its build context in the system
// cache under the predicted key (load.PredictContext), whose bytes the prediction holds, so a trim or another run can
// not take them between the decision and the read. Such a build is awaited only until its go env has started; any other
// until its goroutine ends. Once go env answers, its own checks decide (load.GoEnvironment.ShareModulesOf,
// load.GoEnvironment.KeepContext): a prediction the answer contradicts runs the build's own command then, which, after
// the first load began, the run's account names as late (load.Children).
func (starts *buildStarts) start(extra project.Build) *buildGoEnvironment {
	build := &buildGoEnvironment{}
	build.ended.Add(1)
	starts.awaited.Add(1)
	starts.joined.Go(func() {
		defer build.ended.Done()
		awaited := sync.OnceFunc(starts.awaited.Done)
		defer awaited()
		if starts.oneAtATime != nil {
			starts.oneAtATime.Lock()
			defer starts.oneAtATime.Unlock()
		}
		timed := starts.collector != nil
		environment := extraBuildEnvironment(starts.workspace, extra)
		predicted := load.PredictContext(starts.contexts, starts.host, environment, timed)
		build.forksNothingAfter = predicted.Kept() &&
			load.ModulesPredictedShared(starts.host, starts.workspace.Root, environment, timed)
		var goEnvironmentStarted func()
		if build.forksNothingAfter {
			goEnvironmentStarted = awaited
		}
		// Ignored: when go env fails the build reads it again as it runs, and fails with go env's own words.
		goEnvironment, _ := load.ReadGoEnvironmentTelling(starts.workspace.Root, environment, nil, starts.children,
			goEnvironmentStarted)
		if goEnvironment != nil {
			goEnvironment.ShareModulesOf(starts.host, timed)
			goEnvironment.KeepContext(predicted)
			goEnvironment.FetchListerState(starts.collector)
		}
		build.goEnvironment = goEnvironment
	})
	return build
}
