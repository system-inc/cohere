package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/go_language/staticcheck_configuration"
	"github.com/system-inc/cohere/internal/go_language/timing"
)

// GoEnvironment is go env's answer for a workspace under an environment, as ReadGoEnvironment reads it: the whole
// environment every go command of a load runs with, the build target, the go environment the inputs record names, and
// every variable of goEnvironmentNames by name. The project's description asks for it beside its census and its reads,
// and judges the census's constraints by it (ExcludedByConstraints), and a load handed it through Caching asks go env
// nothing, so a run asks once where it asked twice (#f53yra8); the load's lister, its kept listing and the engine's
// findings cache read their variables from it too, so a build asks go env once where it asked up to five times
// (#4rpt1jh). Nothing a run writes moves it: go env reads the environment, the go env files and the go.mod and go.work
// go finds, and a run rewrites only Go files.
type GoEnvironment struct {
	workspace       string
	environment     []string
	fullEnvironment []string
	buildContext    *build.Context
	reported        reportedEnvironment

	// answer is go env's answer of goEnvironmentNames under the environment as given, by name, `GOFLAGS` as given
	// too (see value).
	answer map[string]string

	// guard runs every go list of a load under it beside a side copy of the go.work in use, so none rewrites the
	// tree's go.work.sum, and as it is when go reads no go.work (see workspaceSumGuard). go env runs without it: it
	// reads the go.work's path and never its sums, and every reader of `GOWORK` reads the tree's own.
	guard *workspaceSumGuard

	// listerState is the two go commands the in-process lister reads under the answer, run once for every load under
	// it, and early when the run asks (FetchListerState), the build list the host's when an extra build's reads it
	// (ShareModulesOf). A pointer, so a copy of the answer, as a test makes, shares it.
	listerState *listerState
}

// CgoToolchainNames are what go env reports of the C toolchain the cgo tool runs, which the cgo-translated loader
// (LoaderCgoTranslated) type-checks a package with cgo files from the outputs of: the compilers, their flags, the
// archiver and pkg-config. Every key of a package checked so holds them (#syscache), since each can change what the
// package checks to. `GOGCCFLAGS` is left out: go env reports it with a `-ffile-prefix-map` naming a build directory
// of its own each time, so it would differ every run, and the rest of it follows `GOARCH` and `CC`, which keys hold.
var CgoToolchainNames = []string{
	"CC", "CXX", "FC", "AR", "GCCGO", "PKG_CONFIG", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS",
	"CGO_LDFLAGS",
}

// goEnvironmentNames are every variable a reader of a GoEnvironment reads, asked of go env in one command: the kept
// listing's key (listingEnvironment), which holds the lister's `GOVERSION`, `GODEBUG`, `GOFLAGS`, `GOWORK` and
// `GOFIPS140`, and the findings cache's `GOROOT` and build names (go_engine's buildNames), then the build target's
// go env file and build cache, which the inputs record names (constraintContext), and the C toolchain the keys hold
// under the cgo-translated loader (CgoToolchainNames).
var goEnvironmentNames = slices.Concat(listingEnvironment, []string{"GOENV", "GOCACHE"}, CgoToolchainNames)

// ReadGoEnvironment asks go env in directory under the current environment plus environment, once, and makes the
// whole environment every go command of a load runs with from its `GOFLAGS`: the same flags, -pgo=off last, as
// go/packages lists, since a command with a default.pgo profile would otherwise have go list print a second variant
// of every package the command builds. -trimpath stays as given, since no cgo tool runs and go list prints nothing
// differently for it. go env is asked under the environment as given, and nothing it reports but `GOFLAGS` itself
// would differ under the whole one: go env defines no flag of `GOFLAGS` but -n and -x, and skips every other (see
// value), so the load's lister and kept listing, which asked under the whole environment, read the same answer with
// `GOFLAGS` rewritten. held is what the build context opens a file from when ExcludedByConstraints judges it, only
// stored as the context is made and never called here, so a caller may ask before it holds the bytes; a load reads it
// through contextFor. children is the run's account of the go commands it starts (see Children), which go env here and
// every go list under the answer are recorded in, nil to record none.
func ReadGoEnvironment(directory string, environment []string, held Held, children *Children) (*GoEnvironment, error) {
	return ReadGoEnvironmentTelling(directory, environment, held, children, nil)
}

// ReadGoEnvironmentTelling is ReadGoEnvironment that calls started, when it is not nil, once go env's child has
// started (askGoEnvironment), and goes on to read its answer: the early start waits for a build that forks nothing
// after its go env only until then, and lets its go env run on beside the host's listing (#sfjj2rh). started is called
// once on every path, as the call returns at the latest, so a go env that never started a child is told too.
func ReadGoEnvironmentTelling(
	directory string,
	environment []string,
	held Held,
	children *Children,
	started func(),
) (*GoEnvironment, error) {
	var tell func()
	if started != nil {
		tell = sync.OnceFunc(started)
		defer tell()
	}
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, absoluteError
	}
	given := append(os.Environ(), environment...)
	answer, askError := askGoEnvironment(workspace, given, children, tell)
	if askError != nil {
		return nil, askError
	}
	goEnvironmentHooks.called(workspace, environment, answer)
	// The build cache holds the cgo tool's outputs a load reads, which the staticcheck.conf a package's rules read is
	// never found beside, so the config analyzer skips them, and this is where it learns the run's (#sc5qmy7).
	staticcheck_configuration.AddBuildCache(answer["GOCACHE"])
	buildContext, reported, contextError := constraintContext(workspace, answer, held)
	if contextError != nil {
		return nil, contextError
	}
	reported.goFlags = append(reported.goFlags, "-pgo=off")
	fullEnvironment := withGoFlags(given, reported.goFlags)
	return &GoEnvironment{
		workspace:       workspace,
		environment:     slices.Clone(environment),
		fullEnvironment: fullEnvironment,
		buildContext:    buildContext,
		reported:        reported,
		answer:          answer,
		guard:           newWorkspaceSumGuard(fullEnvironment, answer, children),
		listerState:     &listerState{},
	}, nil
}

// askGoEnvironment is go env's answer of goEnvironmentNames in workspace under environment, the whole environment,
// by name: the one go env the engine runs for a run's build (#4rpt1jh). It asks with `PWD` set to workspace, as
// runList does, so the go.mod and go.work it reports, and the modules a go.work uses below it, are spelled under
// workspace as the load names it, never through a link the caller's working directory went through (#hd5bsqh): a run
// through a linked root then names its inputs as a run at the real root does. The command is recorded in children, and
// started, when it is not nil, is called once its child has started, or has failed to. A test's run whose children
// keep answers (Children.Answers) reads one kept under the same key in place of starting the child, recorded and told
// all the same, so every count and hook a test reads sees the ask.
func askGoEnvironment(
	workspace string,
	environment []string,
	children *Children,
	started func(),
) (map[string]string, error) {
	goEnvironmentWatches.counted(workspace)
	arguments := append([]string{"env", "-json"}, goEnvironmentNames...)
	children.started(arguments)
	defer children.ended()
	answers := children.GoAnswers()
	if answers == nil {
		return runGoEnvironment(workspace, environment, arguments, started)
	}
	answer, askError := answers.goEnvironment(workspace, environment, func() (map[string]string, error) {
		return runGoEnvironment(workspace, environment, arguments, started)
	})
	// A kept answer started no child, and is told as one that has: started may be called again, as the early start's
	// is a once (ReadGoEnvironmentTelling).
	if started != nil {
		started()
	}
	return answer, askError
}

// runGoEnvironment runs go env with arguments in workspace under environment, the whole one, and reads its answer
// (askGoEnvironment), calling started, when it is not nil, once its child has started, or has failed to.
func runGoEnvironment(
	workspace string,
	environment []string,
	arguments []string,
	started func(),
) (map[string]string, error) {
	command := exec.Command("go", arguments...)
	command.Dir = workspace
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	var output bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &output
	command.Stderr = &standardError
	// Start returns only once the child has run exec, or failed to: syscall.forkExec opens a pipe whose ends close on
	// exec, forks, and reads the pipe until exec closes the child's end, or the child writes why it failed. So the
	// child no longer shares this process's pages once Start returns, and the copy-on-write fault the early start
	// avoids is behind it (#sktb2y8). Wait follows on every path that started it.
	startError := command.Start()
	if started != nil {
		started()
	}
	if startError != nil {
		return nil, fmt.Errorf("reading the go environment with go env in %s: %w", workspace, startError)
	}
	if waitError := command.Wait(); waitError != nil {
		return nil, fmt.Errorf("reading the go environment with go env in %s: %w: %s", workspace, waitError,
			strings.TrimSpace(standardError.String()))
	}
	answer := map[string]string{}
	if decodeError := json.Unmarshal(output.Bytes(), &answer); decodeError != nil {
		return nil, fmt.Errorf("reading the go environment with go env in %s: %w", workspace, decodeError)
	}
	return answer, nil
}

// withGoFlags is environment without any `GOFLAGS`, then `GOFLAGS` as flags, so the value is the one go reads
// whatever the go env file says, as long as flags are not empty.
func withGoFlags(environment []string, flags []string) []string {
	result := []string{}
	for _, variable := range environment {
		if !strings.HasPrefix(variable, "GOFLAGS=") {
			result = append(result, variable)
		}
	}
	return append(result, "GOFLAGS="+strings.Join(flags, " "))
}

// answers reports whether the answer is for workspace, absolute, under environment, so a load asking for another
// never reads it.
func (goEnvironment *GoEnvironment) answers(workspace string, environment []string) bool {
	return goEnvironment != nil && goEnvironment.workspace == workspace &&
		slices.Equal(goEnvironment.environment, environment)
}

// value is what go env reports of name, one of goEnvironmentNames, under the whole environment every go command of a
// load runs with: the answer under the environment as given, but for `GOFLAGS`, which is the whole environment's own,
// -pgo=off last. go env prints `GOFLAGS` as it reads it, raw, and reads no flag of it but -n and -x, which it skips
// besides when it is not go env's own (cmd/go's envcmd and base.SetFromGOFLAGS), so -pgo=off moves nothing else it
// reports (#4rpt1jh). A test pins it (TestGoEnvironmentUnderTheWholeEnvironmentIsTheGivenOnes).
func (goEnvironment *GoEnvironment) value(name string) string {
	if name == "GOFLAGS" {
		return strings.Join(goEnvironment.reported.goFlags, " ")
	}
	return goEnvironment.answer[name]
}

// Values is go env's answer of every variable a reader reads, by name, as it reports them under the environment as
// given, a copy: `GOFLAGS` is the given flags, without the -pgo=off every load adds, which the findings cache's key
// has always read.
func (goEnvironment *GoEnvironment) Values() map[string]string {
	return maps.Clone(goEnvironment.answer)
}

// Target is the build target go env reported, a copy of the context the census judged the files by, and the
// `GOFLAGS` every load under the answer runs with, so the engine reads the host's build (project.HostBuild) from
// what its loads read rather than asking go env again.
func (goEnvironment *GoEnvironment) Target() (*build.Context, []string) {
	buildContext := *goEnvironment.buildContext
	return &buildContext, slices.Clone(goEnvironment.reported.goFlags)
}

// FetchListerState runs the two go commands the in-process lister reads in the workspace under the whole environment,
// now, unless they have run already, so every load under the answer reads what they printed and starts neither: the
// run fetches each build's before the host's first load, while the heap is small (#sktb2y8). The commands, their
// environment and their guard are the ones a load would start, only earlier. What they spend, and what starting them
// and reading their answers costs this process, is added to collector, by whichever call ran them, so it is counted
// once.
func (goEnvironment *GoEnvironment) FetchListerState(collector *timing.Collector) {
	goEnvironment.listerState.fetch(goEnvironment, collector)
}

// ShareModulesOf has every lister under the answer read host's `go list -m -json all`, its output and its failure,
// in place of running its own, when modulesSharedWith says it prints the same, and reports whether it does: an extra
// build's go env, read beside the host's before the first load, then forks one go list where it forked two
// (#sfjj2rh). host is the host's answer, nil for none, and timed whether the run is under `--timing`. Called once the
// answer is read and before anything fetches its lister state, as the early start does. A load that reads an answer
// of its own (PackagesCached, when the one it is handed does not answer its environment) shares nothing.
func (goEnvironment *GoEnvironment) ShareModulesOf(host *GoEnvironment, timed bool) bool {
	if host == nil || host == goEnvironment || !modulesSharedWith(goEnvironment, host, timed) {
		return false
	}
	goEnvironment.listerState.modulesFrom = host
	return true
}

// contextFor is the build target as a build context that opens a file held holds from its held bytes, and any other
// on disk, whichever bytes the asker held.
func (goEnvironment *GoEnvironment) contextFor(held Held) *build.Context {
	buildContext := *goEnvironment.buildContext
	buildContext.OpenFile = held.openFile()
	return &buildContext
}
