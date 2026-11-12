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
	"sync/atomic"
)

// GoEnvironment is go env's answer for a workspace under an environment, as ReadGoEnvironment reads it: the whole
// environment every go command of a load runs with, the build target, the go environment the inputs record names, and
// every variable of goEnvironmentNames by name. The project's census asks for it (ExcludedByConstraints), and a load
// handed it through Caching asks go env nothing, so a run asks once where it asked twice (#f53yra8); the load's
// lister, its kept listing and the engine's findings cache read their variables from it too, so a build asks go env
// once where it asked up to five times (#4rpt1jh). Nothing a run writes moves it: go env reads the environment, the go
// env files and the go.mod and go.work go finds, and a run rewrites only Go files.
type GoEnvironment struct {
	workspace       string
	environment     []string
	fullEnvironment []string
	buildContext    *build.Context
	reported        reportedEnvironment

	// answer is go env's answer of goEnvironmentNames under the environment as given, by name, `GOFLAGS` as given
	// too (see value).
	answer map[string]string
}

// goEnvironmentNames are every variable a reader of a GoEnvironment reads, asked of go env in one command: the kept
// listing's key (listingEnvironment), which holds the lister's `GOVERSION`, `GODEBUG`, `GOFLAGS`, `GOWORK` and
// `GOFIPS140`, and the findings cache's `GOROOT` and build names (go_engine's buildNames), and then the build
// target's go env file and build cache, which the inputs record names (constraintContext).
var goEnvironmentNames = slices.Concat(listingEnvironment, []string{"GOENV", "GOCACHE"})

// ReadGoEnvironment asks go env in directory under the current environment plus environment, once, and makes the
// whole environment every go command of a load runs with from its `GOFLAGS`: the same flags, -pgo=off last, as
// go/packages lists, since a command with a default.pgo profile would otherwise have go list print a second variant
// of every package the command builds. -trimpath stays as given, since no cgo tool runs and go list prints nothing
// differently for it. go env is asked under the environment as given, and nothing it reports but `GOFLAGS` itself
// would differ under the whole one: go env defines no flag of `GOFLAGS` but -n and -x, and skips every other (see
// value), so the load's lister and kept listing, which asked under the whole environment, read the same answer with
// `GOFLAGS` rewritten. held is what the build context opens a file from when ExcludedByConstraints reads it directly;
// a load reads it through contextFor.
func ReadGoEnvironment(directory string, environment []string, held Held) (*GoEnvironment, error) {
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, absoluteError
	}
	given := append(os.Environ(), environment...)
	answer, askError := askGoEnvironment(workspace, given)
	if askError != nil {
		return nil, askError
	}
	buildContext, reported, contextError := constraintContext(workspace, answer, held)
	if contextError != nil {
		return nil, contextError
	}
	reported.goFlags = append(reported.goFlags, "-pgo=off")
	return &GoEnvironment{
		workspace:       workspace,
		environment:     slices.Clone(environment),
		fullEnvironment: withGoFlags(given, reported.goFlags),
		buildContext:    buildContext,
		reported:        reported,
		answer:          answer,
	}, nil
}

// askGoEnvironment is go env's answer of goEnvironmentNames in workspace under environment, the whole environment,
// by name: the one go env the engine runs for a run's build (#4rpt1jh). It asks in workspace with the process's own
// `PWD`, as the census, the kept listing and the findings cache always asked, so the go.mod and go.work it reports
// are spelled as getcwd spells workspace unless `PWD` is workspace already.
func askGoEnvironment(workspace string, environment []string) (map[string]string, error) {
	goEnvironmentAsked(workspace)
	command := exec.Command("go", append([]string{"env", "-json"}, goEnvironmentNames...)...)
	command.Dir = workspace
	command.Env = environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("reading the go environment with go env in %s: %w: %s", workspace, runError,
			strings.TrimSpace(standardError.String()))
	}
	answer := map[string]string{}
	if decodeError := json.Unmarshal(output, &answer); decodeError != nil {
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

// contextFor is the build target as a build context that opens a file held holds from its held bytes, and any other
// on disk, whichever bytes the asker held.
func (goEnvironment *GoEnvironment) contextFor(held Held) *build.Context {
	buildContext := *goEnvironment.buildContext
	buildContext.OpenFile = held.openFile()
	return &buildContext
}

// goEnvironmentWatches are, by workspace, the counts of go env's asks a test watches (WatchGoEnvironmentAsks), under
// goEnvironmentWatchesLock. Empty but in a test.
var (
	goEnvironmentWatchesLock sync.Mutex
	goEnvironmentWatches     = map[string]*atomic.Int64{}
)

// WatchGoEnvironmentAsks counts every go env askGoEnvironment runs in workspace, absolute, until stop is called, for a
// test holding a whole run to one ask per build. Only a test calls it; it is exported for the engine's tests, which
// run the whole pipeline over a fixture, and keyed by workspace, since tests in one process run many workspaces at
// once.
func WatchGoEnvironmentAsks(workspace string) (asks *atomic.Int64, stop func()) {
	asks = &atomic.Int64{}
	goEnvironmentWatchesLock.Lock()
	defer goEnvironmentWatchesLock.Unlock()
	goEnvironmentWatches[workspace] = asks
	return asks, func() {
		goEnvironmentWatchesLock.Lock()
		defer goEnvironmentWatchesLock.Unlock()
		delete(goEnvironmentWatches, workspace)
	}
}

// goEnvironmentAsked adds one ask in workspace to the count a test watches there, if any.
func goEnvironmentAsked(workspace string) {
	goEnvironmentWatchesLock.Lock()
	defer goEnvironmentWatchesLock.Unlock()
	if asks := goEnvironmentWatches[workspace]; asks != nil {
		asks.Add(1)
	}
}
