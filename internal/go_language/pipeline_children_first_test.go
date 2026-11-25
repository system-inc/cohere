package go_language

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// A run of every build over a module whose census needs two extra builds starts every go command it needs
// before the host's first load (#sktb2y8): the host's lister state, and each build's go env and lister state, so its
// lint record counts no go command started after that load began. On darwin each child started once the heap has grown
// makes every resident page fault again. A build that read its go env or its lister state itself, as each did before,
// would be counted and named there.
func TestARunWithBuildsStartsEveryGoCommandBeforeItsFirstLoad(test *testing.T) {
	test.Parallel()
	engine, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings)
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the run checked %d builds, want the host's and two extra", len(engine.buildRuns))
	}
	lint := lintRecordOf(test, stream)
	if lint.ChildProcessesAfterLoad != 0 || len(lint.ChildProcessesAfterLoadNamed) != 0 {
		test.Errorf("the run started %d go commands after its first load began, want 0: %s",
			lint.ChildProcessesAfterLoad, strings.Join(lint.ChildProcessesAfterLoadNamed, "; "))
	}
}

// A run of every build over a module whose two extra builds' build lists print the host's runs one
// `go list -m -json all`, the host's, which each build reads in place of its own (#sfjj2rh), and still starts no go
// command after its first load began and finds what its expected stream holds, which each build listing its own wrote.
func TestARunsExtraBuildsReadTheHostsBuildList(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	lists, stop := load.WatchBuildLists(root)
	defer stop()
	engine, stream := runOver(test, root, exitFindings)
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the run checked %d builds, want the host's and two extra", len(engine.buildRuns))
	}
	if count := lists.Load(); count != 1 {
		test.Errorf("the run ran %d build lists, want the host's alone", count)
	}
	lint := lintRecordOf(test, stream)
	if lint.ChildProcessesAfterLoad != 0 || len(lint.ChildProcessesAfterLoadNamed) != 0 {
		test.Errorf("the run started %d go commands after its first load began, want 0: %s",
			lint.ChildProcessesAfterLoad, strings.Join(lint.ChildProcessesAfterLoadNamed, "; "))
	}
	expected, readError := os.ReadFile(expectedStreamPath("other-builds"))
	if readError != nil {
		test.Fatal(readError)
	}
	if found, want := findingLines(stream), findingLines(string(expected)); !slices.Equal(found, want) {
		test.Errorf("the run found\n%s\nwant\n%s", strings.Join(found, ""), strings.Join(want, ""))
	}
}

// A run of every build keeps each build's build context in the system cache (#sfjj2rh): a first run over an empty one
// runs the three, the host's and the two extra builds', and keeps them, and a second over the same system cache runs
// none, forks no go list for an extra build at all, finds what the first found, and starts no go command after its
// first load began. A run under `--no-system-cache` runs all three again.
func TestAWarmSystemCacheRunsNoBuildContext(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemCacheRoot := test.TempDir()
	contexts, stopContexts := load.WatchBuildContexts(root)
	defer stopContexts()
	lists, stopLists := load.WatchBuildLists(root)
	defer stopLists()
	run := func(extra ...string) string {
		test.Helper()
		arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"},
			extra...)
		request, parseError := parseInvocation(arguments)
		if parseError != nil {
			test.Fatal(parseError)
		}
		request.standardLibrary.systemCacheRoot = systemCacheRoot
		contexts.Store(0)
		lists.Store(0)
		engine, standardOutput, standardError, exitCode := runOverRequest(test, request, rules.All(), nil)
		if exitCode != exitFindings {
			test.Fatalf("the run exited %d, want %d:\n%s\n%s", exitCode, exitFindings, standardOutput, standardError)
		}
		if len(engine.buildRuns) != 3 {
			test.Fatalf("the run checked %d builds, want the host's and two extra", len(engine.buildRuns))
		}
		return normalize(standardOutput, root)
	}
	cold := run()
	if ran := contexts.Load(); ran != 3 {
		test.Errorf("the run over an empty system cache ran %d build contexts, want the three builds'", ran)
	}
	warm := run()
	if ran := contexts.Load(); ran != 0 {
		test.Errorf("the run over a warm system cache ran %d build contexts, want none", ran)
	}
	if ran := lists.Load(); ran != 1 {
		test.Errorf("the run over a warm system cache ran %d build lists, want the host's alone", ran)
	}
	if found, want := findingLines(warm), findingLines(cold); !slices.Equal(found, want) {
		test.Errorf("the warm run found\n%s\nwant the cold run's\n%s", strings.Join(found, ""), strings.Join(want, ""))
	}
	if lint := lintRecordOf(test, warm); lint.ChildProcessesAfterLoad != 0 {
		test.Errorf("the warm run started %d go commands after its first load began, want 0: %s",
			lint.ChildProcessesAfterLoad, strings.Join(lint.ChildProcessesAfterLoadNamed, "; "))
	}
	run("--no-system-cache")
	if ran := contexts.Load(); ran != 3 {
		test.Errorf("the run under --no-system-cache ran %d build contexts, want the three builds'", ran)
	}
}

// A run of one build under `--timing` starts nothing before its first load, and its lister's two go commands start
// inside it, which the lint record counts and names, so the count is of commands that ran and not a field never set.
// `--timing` is the run that still shows the count live: it keeps the lister state inside the first load, so the load's
// measurement of its children's time holds its own commands, where a run not timed fetches it beside its census and
// its reads (#q9ezwvw) and reads 0, as the end-to-end streams pin.
func TestARunOfOneBuildNamesItsListersCommands(test *testing.T) {
	test.Parallel()
	_, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "clean")), exitClean, "--timing")
	lint := lintRecordOf(test, stream)
	named := slices.Sorted(slices.Values(lint.ChildProcessesAfterLoadNamed))
	want := []string{"go list -e -f", "go list -m -json"}
	if lint.ChildProcessesAfterLoad != len(want) || !slices.Equal(named, want) {
		test.Errorf("the run counted %d go commands after its first load began, named %q, want %d, named %q",
			lint.ChildProcessesAfterLoad, named, len(want), want)
	}
}

// runWithSystemCache is a run of every build over root, under `--no-cache` and extra, reading and writing the system
// cache at systemCacheRoot, which runs given the same root share, with configure, when not nil, given the pipeline
// before it runs, and its normalized stream and exit.
func runWithSystemCache(
	test *testing.T,
	root string,
	systemCacheRoot string,
	configure func(*pipeline),
	extra ...string,
) (*pipeline, string, int) {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"}, extra...)
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	request.standardLibrary.systemCacheRoot = systemCacheRoot
	engine, standardOutput, _, exitCode := runOverRequest(test, request, rules.All(), configure)
	return engine, normalize(standardOutput, root), exitCode
}

// heldUntilLoadBegins is a hook of go env's answers (load.HookGoEnvironmentAnswers) that holds the windows build's
// answer until loadBegan is closed, or a minute has passed, then hands it to change, when not nil, and reports in
// released whether the load's beginning released it.
func heldUntilLoadBegins(
	loadBegan <-chan struct{},
	released *atomic.Bool,
	change func(answer map[string]string),
) func(environment []string, answer map[string]string) {
	return func(environment []string, answer map[string]string) {
		if !slices.Contains(environment, "GOOS=windows") {
			return
		}
		select {
		case <-loadBegan:
			released.Store(true)
		case <-time.After(time.Minute):
		}
		if change != nil {
			change(answer)
		}
	}
}

// watchingLoadBegin has a pipeline close loadBegan as its first load begins, once the run's account counts every go
// command started after as late (load.Children.LoadBegins), then call then, when not nil.
func watchingLoadBegin(loadBegan chan struct{}, then func()) func(*pipeline) {
	return func(engine *pipeline) {
		var once sync.Once
		engine.watchPrelude = func(step preludeStep) {
			if step != preludeLoadBegan {
				return
			}
			once.Do(func() {
				engine.described.children.LoadBegins()
				close(loadBegan)
			})
			if then != nil {
				then()
			}
		}
	}
}

// requirePredictions fails unless every extra build of engine's run was predicted to fork nothing after its go env,
// when want, or none was.
func requirePredictions(test *testing.T, engine *pipeline, want bool) {
	test.Helper()
	if len(engine.buildGoEnvironments) != 2 {
		test.Fatalf("the run started %d extra builds, want two", len(engine.buildGoEnvironments))
	}
	for name, build := range engine.buildGoEnvironments {
		if build.answer(); build.forksNothingAfter != want {
			test.Errorf("the %s build was predicted to fork nothing after its go env: %t, want %t", name,
				build.forksNothingAfter, want)
		}
	}
}

// requireNoLateCommand fails unless the run's lint record names no go command started after its first load began.
func requireNoLateCommand(test *testing.T, stream string, run string) {
	test.Helper()
	if lint := lintRecordOf(test, stream); lint.ChildProcessesAfterLoad != 0 {
		test.Errorf("the %s run started %d go commands after its first load began, want 0: %s", run,
			lint.ChildProcessesAfterLoad, strings.Join(lint.ChildProcessesAfterLoadNamed, "; "))
	}
}

// A run over an empty system cache predicts no extra build to fork nothing after its go env, so it waits each out
// whole before its first load, which starts no go command late; a run over the system cache the first kept predicts
// both, and its first load begins before the windows build's go env has answered, which a hook holds until then, and
// still starts no go command late, and finds what the first found (#sfjj2rh).
func TestAWarmRunWaitsForABuildThatForksNothingAfterOnlyUntilItsGoEnvironmentStarts(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemCacheRoot := test.TempDir()
	cold, coldStream, _ := runWithSystemCache(test, root, systemCacheRoot, nil)
	requirePredictions(test, cold, false)
	requireNoLateCommand(test, coldStream, "cold")

	loadBegan := make(chan struct{})
	var released atomic.Bool
	stop := load.HookGoEnvironmentAnswers(root, heldUntilLoadBegins(loadBegan, &released, nil))
	defer stop()
	warm, warmStream, _ := runWithSystemCache(test, root, systemCacheRoot, watchingLoadBegin(loadBegan, nil))
	requirePredictions(test, warm, true)
	if !released.Load() {
		test.Error("the warm run's first load did not begin before the windows build's go env answered")
	}
	requireNoLateCommand(test, warmStream, "warm")
	if found, want := findingLines(warmStream), findingLines(coldStream); !slices.Equal(found, want) {
		test.Errorf("the warm run found\n%s\nwant the cold run's\n%s", strings.Join(found, ""), strings.Join(want, ""))
	}
}

// A build predicted to fork nothing after its go env whose own answer contradicts the prediction, as a go env file of
// its own that the host's does not read would, runs its own build list and build context once the answer comes, after
// the run's first load began, which the lint record names as late, and the run finds what a run over an empty system
// cache found: the checks of the answer decide, never the prediction.
func TestAContradictedPredictionRunsTheBuildsCommandsLate(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemCacheRoot := test.TempDir()
	_, coldStream, _ := runWithSystemCache(test, root, systemCacheRoot, nil)

	loadBegan := make(chan struct{})
	var released atomic.Bool
	stop := load.HookGoEnvironmentAnswers(root, heldUntilLoadBegins(loadBegan, &released,
		func(answer map[string]string) {
			answer["GOENV"] = filepath.Join(root, "another-go-env-file")
		}))
	defer stop()
	warm, warmStream, _ := runWithSystemCache(test, root, systemCacheRoot, watchingLoadBegin(loadBegan, nil))
	requirePredictions(test, warm, true)
	if !released.Load() {
		test.Fatal("the run's first load did not begin before the windows build's go env answered")
	}
	named := lintRecordOf(test, warmStream).ChildProcessesAfterLoadNamed
	for _, command := range []string{"go list -e -f", "go list -m -json"} {
		if !slices.Contains(named, command) {
			test.Errorf("the run named %q as started after its first load began, want %s among them", named, command)
		}
	}
	if found, want := findingLines(warmStream), findingLines(coldStream); !slices.Equal(found, want) {
		test.Errorf("the run found\n%s\nwant the cold run's\n%s", strings.Join(found, ""), strings.Join(want, ""))
	}
}

// errorStopped is the panic a test stops a run with as its first load begins.
var errorStopped = errors.New("stopped as the first load began")

// A run that stops once its first load has begun, here with a panic, joins every go command it started before it
// returns: the windows build, predicted to fork nothing after its go env and contradicted by its answer, starts its own
// commands once the first load has begun, the first of which is held running until the run's join begins, and the run
// stops only once it has started. The run then joins it, so none is running once it has returned; a run that did not
// join would leave it held.
func TestARunThatStopsAfterItsFirstLoadBeganLeavesNoGoCommandRunning(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemCacheRoot := test.TempDir()
	runWithSystemCache(test, root, systemCacheRoot, nil)

	loadBegan := make(chan struct{})
	var released atomic.Bool
	stop := load.HookGoEnvironmentAnswers(root, heldUntilLoadBegins(loadBegan, &released,
		func(answer map[string]string) {
			answer["GOENV"] = filepath.Join(root, "another-go-env-file")
		}))
	defer stop()
	lateStarted := make(chan struct{})
	lateEnds := make(chan struct{})
	letLateEnd := sync.OnceFunc(func() { close(lateEnds) })
	// On every way out too, so the held command ends before the test's cleanup joins what the run started.
	defer letLateEnd()
	var held atomic.Bool
	var joined atomic.Bool
	var stopped *pipeline
	func() {
		defer func() {
			if recovered := recover(); recovered != errorStopped {
				panic(recovered)
			}
		}()
		runWithSystemCache(test, root, systemCacheRoot, func(engine *pipeline) {
			stopped = engine
			holdOnce := sync.OnceFunc(func() {
				held.Store(true)
				close(lateStarted)
				<-lateEnds
			})
			engine.described.children.WatchStarted(func(name string) {
				select {
				case <-loadBegan:
				default:
					return
				}
				if name == "go list -m -json" || name == "go list -e -f" {
					holdOnce()
				}
			})
			engine.watchJoin = func() {
				joined.Store(true)
				letLateEnd()
			}
			watchingLoadBegin(loadBegan, func() {
				select {
				case <-lateStarted:
				case <-time.After(time.Minute):
				}
				panic(errorStopped)
			})(engine)
		})
		test.Fatal("the run returned though it was stopped")
	}()
	if !released.Load() || !held.Load() {
		test.Fatal("the windows build started no command of its own once the first load began, so nothing was held")
	}
	if !joined.Load() {
		test.Error("the stopped run returned without joining the go commands it started")
	}
	if running := stopped.described.children.Running(); running != 0 {
		test.Errorf("%d go commands are running once the stopped run has returned, want none", running)
	}
}
