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
// Every one is joined before the host's first load (describedProject.awaitStarted), or before describeProject returns
// an error, so no go command outlives a refused run, and none of the builds starts after the first load begins.
type earlyStart struct {
	// hostAnswered ends once the host's go env has answered, goEnvironment and goEnvironmentError its answer, read
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

	// buildGoEnvironments are each extra build's go env, by the build's index among the extras, read with its lister
	// state, nil where go env failed, and nil whole when no batch was started (startBuilds). Written by the batch and
	// read only once started has ended.
	buildGoEnvironments []*load.GoEnvironment

	// started counts every goroutine started here, the batch's too, so one wait joins them all.
	started sync.WaitGroup
}

// startEarly starts the host's go env in workspace under its environment, with held as what its build context opens a
// file through (load.ReadGoEnvironment), then, when fetchesListerState, the host's lister state, and, when readsPorts,
// the toolchain's ports beside it, each on a goroutine of its own, every go command recorded in children.
func startEarly(
	workspace project.Workspace,
	held load.Held,
	children *load.Children,
	fetchesListerState bool,
	readsPorts bool,
) *earlyStart {
	early := &earlyStart{}
	early.hostAnswered.Add(1)
	early.started.Go(func() {
		early.goEnvironment, early.goEnvironmentError = load.ReadGoEnvironment(workspace.Root,
			workspace.Environment(), held, children)
		early.hostAnswered.Done()
		if fetchesListerState && early.goEnvironment != nil {
			// Not timed: a run under `--timing` fetches its lister state where it always has, so each measurement of
			// its children's CPU is of its own commands alone (startChildrenFirst).
			early.goEnvironment.FetchListerState(nil)
		}
	})
	if readsPorts {
		early.portsAnswered.Add(1)
		early.started.Go(func() {
			defer early.portsAnswered.Done()
			early.ports, early.portsError = project.ReadPorts(workspace.Root, workspace.Environment())
		})
	}
	return early
}

// hostGoEnvironment is the host's go env, and how reading it failed, once it has answered.
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
	early.started.Go(func() {
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
// build on a goroutine of its own, as startChildrenNow does in a run whose description started none, every go command
// recorded in children: a build whose build list prints what host's does, host the host's go env, reads host's and
// runs only its build context (load.GoEnvironment.ShareModulesOf, #sfjj2rh). Called once, from a goroutine started
// counts, so its wait never ends before these are counted.
func (early *earlyStart) startBuilds(
	workspace project.Workspace,
	host *load.GoEnvironment,
	extras []project.Build,
	children *load.Children,
) {
	early.buildGoEnvironments = make([]*load.GoEnvironment, len(extras))
	for index, extra := range extras {
		early.started.Go(func() {
			// Ignored: when go env fails the build reads it again as it runs, and fails with go env's own words.
			goEnvironment, _ := load.ReadGoEnvironment(workspace.Root, extraBuildEnvironment(workspace, extra), nil,
				children)
			if goEnvironment != nil {
				// Not timed: the batch starts only in a run not under `--timing` (startReadingBuilds's startsBuilds).
				goEnvironment.ShareModulesOf(host, false)
				goEnvironment.FetchListerState(nil)
			}
			early.buildGoEnvironments[index] = goEnvironment
		})
	}
}

// wait joins every go command started here.
func (early *earlyStart) wait() {
	early.started.Wait()
}

// extraBuildEnvironment is the environment an extra build's loads run under: the workspace's, then the build's target,
// cgo and flags.
func extraBuildEnvironment(workspace project.Workspace, extra project.Build) []string {
	return append(slices.Clone(workspace.Environment()), extra.Environment()...)
}
