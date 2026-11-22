package go_engine

import (
	"slices"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// earlyStart is the go commands describeProject starts beside its census and its file reads, which wait on neither
// (#q9ezwvw): the host's go env, which needs only the root and the workspace's environment, then its lister state,
// which needs only go env's answer, and, in a run that reads builds, the toolchain's ports (project.ReadPorts), which
// need only the root and the environment, then, once the builds are read, each extra build's go env and lister state,
// the host's build list read in place of its own where it prints the same (#sfjj2rh).
// The host's first load waits for each until it ends, but an extra build predicted to fork nothing after its go env,
// only until its go env has started (describedProject.awaitForked, buildStarts), so none of the builds starts a child
// after the first load begins unless its own answer contradicts the prediction, and every one is joined before the run
// returns (pipeline.joinChildren), or before describeProject returns an error, so no go command outlives a run.
type earlyStart struct {
	// hostAnswered ends once the host's go env has answered and the early start has done writing it, the build
	// context's kept place set (load.GoEnvironment.KeepContext), goEnvironment and goEnvironmentError its answer, read
	// only after.
	hostAnswered       sync.WaitGroup
	goEnvironment      *load.GoEnvironment
	goEnvironmentError error

	// portsAnswered ends once go tool dist list has answered, ports and portsError its answer, read only after; it
	// ends at once, with neither, in a run that reads no builds.
	portsAnswered sync.WaitGroup
	ports         []project.Port
	portsError    error

	// buildsRead ends once the builds the census needs are read (startReadingBuilds), builds, unbuiltReasons and
	// buildsError what readBuilds read, read only after; it ends at once, with none, in a run that reads no builds.
	buildsRead     sync.WaitGroup
	builds         []project.Build
	unbuiltReasons map[string]string
	buildsError    error

	// contexts is where each build's build context is kept across runs, the system cache's store, nil when the run
	// keeps none (load.GoEnvironment.KeepContext).
	contexts load.ListingStore

	// buildGoEnvironments are each extra build's go env, by the build's index among the extras, each read once it is,
	// with its lister state (buildGoEnvironment.answer), and nil whole when no batch was started (startBuilds). The
	// slice is written by the batch's reader and read only once awaited has ended.
	buildGoEnvironments []*buildGoEnvironment

	// started counts every goroutine started here, the batch's too, until it ends, so one wait joins them all, and
	// awaited counts what the host's first load waits for: every one until it ends, but an extra build predicted to
	// fork nothing after its go env, until that has started (buildStarts).
	started sync.WaitGroup
	awaited sync.WaitGroup
}

// goAwaited runs work on a goroutine started and awaited both count until it ends.
func (early *earlyStart) goAwaited(work func()) {
	early.awaited.Add(1)
	early.started.Go(func() {
		defer early.awaited.Done()
		work()
	})
}

// startEarly starts the host's go env in workspace under its environment, with held as what its build context opens a
// file through (load.ReadGoEnvironment), then, when fetchesListerState, the host's lister state, its build context read
// from contexts when a run kept it there, and, when readsPorts, the toolchain's ports beside it, each on a goroutine of
// its own, every go command recorded in children.
func startEarly(
	workspace project.Workspace,
	held load.Held,
	children *load.Children,
	contexts load.ListingStore,
	fetchesListerState bool,
	readsPorts bool,
) *earlyStart {
	early := &earlyStart{contexts: contexts}
	early.hostAnswered.Add(1)
	early.goAwaited(func() {
		early.goEnvironment, early.goEnvironmentError = load.ReadGoEnvironment(workspace.Root,
			workspace.Environment(), held, children)
		keepsContext := fetchesListerState && early.goEnvironment != nil
		// Not timed: a run under `--timing` fetches its lister state where it always has, so each measurement of its
		// children's CPU is of its own commands alone (startChildrenFirst).
		if keepsContext {
			early.goEnvironment.KeepContext(load.PredictContext(contexts, early.goEnvironment, workspace.Environment(),
				false))
		}
		// Handed out only now, its last write done (#e8w514b): KeepContext writes the answer's lister state without a
		// lock, before anything fetches it, so a reader handed the answer first, as a load that lists, reads it as it
		// is written. The fetch below is the lister state's own, under its once, which a load fetching beside it
		// shares.
		early.hostAnswered.Done()
		if keepsContext {
			early.goEnvironment.FetchListerState(nil)
		}
	})
	if readsPorts {
		early.portsAnswered.Add(1)
		early.goAwaited(func() {
			defer early.portsAnswered.Done()
			early.ports, early.portsError = project.ReadPorts(workspace.Root, workspace.Environment())
		})
	}
	return early
}

// hostGoEnvironment is the host's go env, and how reading it failed, once it has answered and the early start has done
// writing it, so whoever it is handed to reads it whole with no await (#e8w514b).
func (early *earlyStart) hostGoEnvironment() (*load.GoEnvironment, error) {
	early.hostAnswered.Wait()
	return early.goEnvironment, early.goEnvironmentError
}

// readPorts is what go tool dist list answered, once it has.
func (early *earlyStart) readPorts() ([]project.Port, error) {
	early.portsAnswered.Wait()
	return early.ports, early.portsError
}

// startReadingBuilds reads the builds the census needs beside the host's (readBuilds) on a goroutine of its own, once
// the host's go env and the ports have answered, beside the file reads: project.BuildsFor takes each file's bytes
// through reader, the description's read of it (heldReads), as soon as that read is done, so no file is opened twice
// (#ghb3rcn). With a go env that failed it reads none, since the run is refused with go env's words, or, when no file
// was readable, never asked. Then, when startsBuilds, it starts each extra build's go env and lister state
// (startBuilds).
func (early *earlyStart) startReadingBuilds(
	workspace project.Workspace,
	census project.Census,
	targets []string,
	startsBuilds bool,
	children *load.Children,
	reader func(file string) ([]byte, error),
) {
	early.buildsRead.Add(1)
	early.goAwaited(func() {
		// Deferred, so the batch is started before the builds are handed back, and a describe that has them has its
		// goroutines counted.
		defer early.buildsRead.Done()
		// Ignored: a go env that failed refuses a run with files to judge in its own words, and one with none reads no
		// builds (describeProject), so none are read here.
		goEnvironment, _ := early.hostGoEnvironment()
		if goEnvironment == nil {
			return
		}
		ports, portsError := early.readPorts()
		if portsError != nil {
			early.buildsError = portsError
			return
		}
		early.builds, early.unbuiltReasons, early.buildsError = readBuilds(census, goEnvironment, ports, targets,
			reader)
		if startsBuilds && early.buildsError == nil && len(early.builds) > 1 {
			early.startBuilds(workspace, goEnvironment, early.builds[1:], children)
		}
	})
}

// awaitBuilds is what startReadingBuilds read, once it has: none, in a run that reads no builds.
func (early *earlyStart) awaitBuilds() ([]project.Build, map[string]string, error) {
	early.buildsRead.Wait()
	return early.builds, early.unbuiltReasons, early.buildsError
}

// startBuilds starts each of extras' go env in workspace under the build's environment, then its lister state, each
// build on a goroutine of its own (buildStarts), as startChildrenNow does in a run whose description started none,
// every go command recorded in children: a build whose build list prints what host's does, host the host's go env,
// reads host's and runs only its build context (load.GoEnvironment.ShareModulesOf, #sfjj2rh), and reads that from the
// system cache when a run kept it there (load.GoEnvironment.KeepContext). Called once, from a goroutine started and
// awaited count, so neither wait ends before these are counted. Not timed: the batch starts only in a run not under
// `--timing` (startReadingBuilds's startsBuilds).
func (early *earlyStart) startBuilds(
	workspace project.Workspace,
	host *load.GoEnvironment,
	extras []project.Build,
	children *load.Children,
) {
	starts := &buildStarts{workspace: workspace, host: host, children: children, contexts: early.contexts,
		joined: &early.started, awaited: &early.awaited}
	early.buildGoEnvironments = make([]*buildGoEnvironment, len(extras))
	for index, extra := range extras {
		early.buildGoEnvironments[index] = starts.start(extra)
	}
}

// wait joins every go command started here.
func (early *earlyStart) wait() {
	early.started.Wait()
}

// awaitForked returns once every go command started here that the host's first load waits for has ended, and every
// extra build predicted to fork nothing after its go env has started that (awaited).
func (early *earlyStart) awaitForked() {
	early.awaited.Wait()
}

// extraBuildEnvironment is the environment an extra build's loads run under: the workspace's, then the build's target,
// cgo and flags.
func extraBuildEnvironment(workspace project.Workspace, extra project.Build) []string {
	return append(slices.Clone(workspace.Environment()), extra.Environment()...)
}
