package load

import (
	"crypto/sha256"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/go_language/timing"
)

// listerCommands are the two go commands the lister needs: the build list, and the build context go list reads files
// under. An array, so its length is a constant a listerState's runs are counted by.
var listerCommands = [...][]string{
	{"list", "-m", "-json", "all"},
	{"list", "-e", "-f", contextTemplate, "unsafe"},
}

// buildList is listerCommands' index of the build list, the one command an answer may read of another's
// (modulesSharedWith), and buildContext its index of the build context, the one a run may read from the system cache
// (KeepContext).
const (
	buildList    = 0
	buildContext = 1
)

// listerState is what listerCommands printed under one GoEnvironment, each command's output and failure by its index,
// fetched once for every load under it (#sktb2y8).
type listerState struct {
	once     sync.Once
	outputs  [][]byte
	failures []error

	// commands are each of listerCommands' own run under the answer, by its index, each run once by whichever state
	// reaches it first: this one, or one whose build list is this answer's.
	commands [len(listerCommands)]listerCommand

	// modulesFrom is the answer whose build list this one reads in place of its own, run under that answer, its
	// environment and its guard, and never here, so this one's guard records nothing for it; its failure is read too.
	// Nil for an answer that lists its own. Set only before the state is first fetched (ShareModulesOf).
	modulesFrom *GoEnvironment

	// predictedContext is where the build context is kept across runs, the system cache's store, under the key
	// predicted for it from the host's go env before this answer's own, and what the store held there then: read in
	// place of running the command when this answer's own key is the predicted one, and written when the command runs
	// and succeeds. Nil to keep none. Set only before the state is first fetched (KeepContext).
	predictedContext *PredictedContext
}

// listerCommand is one of listerCommands as it ran, what it printed and how it failed.
type listerCommand struct {
	once    sync.Once
	output  []byte
	failure error
}

// fetch is what each of listerCommands printed in goEnvironment's workspace under its whole environment, beside its
// guard's copy of the go.work, and how each failed, run at once on the first call, each output shared by every caller
// and only read: the build list another answer's (modulesFrom), the same slice, when the answer reads that one's.
// What they spend is added to collector, the first call's.
func (state *listerState) fetch(goEnvironment *GoEnvironment, collector *timing.Collector) ([][]byte, []error) {
	state.once.Do(func() {
		state.outputs = make([][]byte, len(listerCommands))
		state.failures = make([]error, len(listerCommands))
		// The two run at once, so their children's CPU is read once around both, which a read around each would count
		// twice.
		measureGoCommands(collector, func() {
			var group sync.WaitGroup
			for index := range listerCommands {
				group.Go(func() {
					state.outputs[index], state.failures[index] = state.ran(index, goEnvironment, collector)
				})
			}
			group.Wait()
		})
	})
	return state.outputs, state.failures
}

// ran is what listerCommands[index] printed for goEnvironment, whose state this is, and how it failed, running it
// now unless it has run: the build list under modulesFrom, when the answer reads that one's, and every other command
// under goEnvironment.
func (state *listerState) ran(index int, goEnvironment *GoEnvironment, collector *timing.Collector) ([]byte, error) {
	if index == buildList && state.modulesFrom != nil {
		return state.modulesFrom.listerState.ran(index, state.modulesFrom, collector)
	}
	command := &state.commands[index]
	command.once.Do(func() {
		key, keeps := state.keptContextKey(index, goEnvironment)
		if keeps {
			if kept, stored := state.predictedContext.read(); stored {
				command.output = kept
				return
			}
		}
		listerCommandWatches[index].counted(goEnvironment.workspace)
		measureListing(collector, func() {
			command.output, command.failure = goOutput(goEnvironment.guard, goEnvironment.workspace,
				goEnvironment.fullEnvironment, listerCommands[index])
		})
		if keeps && command.failure == nil {
			// Ignored: a build context the store could not keep is asked of go list again next run, which is all its
			// keeping saves, and the store says why it kept none (the engine's systemCache).
			_ = state.predictedContext.store.Keep(key, command.output)
		}
	})
	return command.output, command.failure
}

// keptContextKey is where listerCommands[index] is kept in the system cache, and whether it is: only the build context,
// only with a store, and only when the key of goEnvironment's own answer is the one predicted from the host's before
// it, so a prediction the answer contradicts, as a toolchain the build's go env names and the host's did not, runs
// the command and keeps nothing.
func (state *listerState) keptContextKey(index int, goEnvironment *GoEnvironment) ([sha256.Size]byte, bool) {
	if index != buildContext || state.predictedContext == nil {
		return [sha256.Size]byte{}, false
	}
	key, keeps := ownContextKey(goEnvironment)
	return key, keeps && key == state.predictedContext.key
}

// ModulesPredictedShared reports whether an extra build's go commands under environment, the caller's entries, in
// workspace are predicted to read host's build list (modulesSharedWith), from what can be compared before the build's
// go env answers (#sfjj2rh): not in a run under `--timing`; the same workspace; host's `GOFIPS140` off, which the
// build's environment, the host's but its target, cgo and flags, keeps; and the build's flags as environment gives
// them, with -pgo=off last as ReadGoEnvironment adds it, the same as host's once every -tags and -pgo is taken out,
// each passing listerFlagsOnly. A build whose environment sets no flags reads its go env file's, which only its go env
// answers, so it is predicted to list its own. The check once the build's go env answers (ShareModulesOf) decides;
// this only says whether the build is waited out whole before the first load.
func ModulesPredictedShared(host *GoEnvironment, workspace string, environment []string, timed bool) bool {
	if timed || host == nil {
		return false
	}
	absolute, absoluteError := filepath.Abs(workspace)
	if absoluteError != nil || absolute != host.workspace {
		return false
	}
	if fips140 := host.value("GOFIPS140"); fips140 != "" && fips140 != "off" {
		return false
	}
	_, variables := givenVariables(environment)
	if variables["GOFLAGS"] == "" {
		return false
	}
	goFlags := append(strings.Fields(variables["GOFLAGS"]), "-pgo=off")
	return slices.Equal(flagsBuildListReads(goFlags), flagsBuildListReads(host.reported.goFlags)) &&
		listerFlagsOnly(strings.Join(goFlags, " ")) == "" && listerFlagsOnly(host.value("GOFLAGS")) == ""
}

// buildListUnread are the names of goEnvironmentNames whose answer the build list never reads, which an extra build's
// answer holds otherwise than the host's while its `go list -m -json all` prints the same bytes: cmd/go's
// listModules, moduleInfo and loadModFile never touch the build context (#sfjj2rh). Each is here for its why.
var buildListUnread = map[string]bool{
	// The target: an extra build sets it.
	"GOOS": true, "GOARCH": true,
	// cgo: an extra build sets it.
	"CGO_ENABLED": true,
	// The flags: an extra build sets them, with its own -tags, and modulesSharedWith compares them as flags.
	"GOFLAGS": true,
	// The architecture levels: go env reports only the target architecture's own, the rest raw (cfg.GetArchEnv).
	"GOAMD64": true, "GOARM": true, "GOARM64": true, "GO386": true,
	// The C compilers: go env defaults them per target (cfg.DefaultCC, cfg.DefaultCXX), and go list -m runs neither.
	"CC": true, "CXX": true,
	// gccgo: go1.27 names it whatever the target (work/gccgo.go), and go list -m never runs it, so one a toolchain
	// defaults per target refuses nothing.
	"GCCGO": true,
}

// modulesSharedWith reports whether build's `go list -m -json all` prints what host's prints, byte for byte, and
// fails as host's fails, so build may read host's in place of running its own (#sfjj2rh): both answers are go env's,
// and the build list reads none of the build context, so it holds when every input it does read is the same. Not in
// a run under `--timing` (timed), whose every step's children's CPU must be of its own commands alone. Not unless
// both answers are of the same workspace, both run with no flag but the lister's (listerFlagsOnly) and with no
// `GOFIPS140` on, so the host's answer never stands in for one the build's target would refuse, as go refuses -race
// or a purego tag beside a `GOFIPS140` snapshot. Not unless their flags are the same once every -tags and -pgo is
// taken out, since the build list reads neither, and an extra build holds -pgo=off twice, its own and the host's
// it copies (project.Build.Flags). And not unless go env answers every other name the same, buildListUnread's left
// out, so a name nobody thought of refuses. That fails closed only over the names go env is asked
// (goEnvironmentNames): a variable go reads and is not asked about, as `GOAUTH`, `GIT_*` or `HOME`, is the same for
// host and build only because both run under the same process environment, the build's with just its four entries
// added (project.Build.Environment). The cheapest comparisons are first, since an extra build's tag builds share all
// but their -tags.
func modulesSharedWith(build *GoEnvironment, host *GoEnvironment, timed bool) bool {
	if timed || build.workspace != host.workspace {
		return false
	}
	for _, goEnvironment := range []*GoEnvironment{build, host} {
		if fips140 := goEnvironment.value("GOFIPS140"); fips140 != "" && fips140 != "off" {
			return false
		}
	}
	if !slices.Equal(flagsBuildListReads(build.reported.goFlags), flagsBuildListReads(host.reported.goFlags)) {
		return false
	}
	for _, goEnvironment := range []*GoEnvironment{build, host} {
		if listerFlagsOnly(goEnvironment.value("GOFLAGS")) != "" {
			return false
		}
	}
	return sameBuildListAnswers(build.answer, host.answer) && sameBuildListAnswers(host.answer, build.answer)
}

// flagsBuildListReads are goFlags, `GOFLAGS`'s flags, without every -tags and -pgo, in order.
func flagsBuildListReads(goFlags []string) []string {
	kept := []string{}
	for _, flag := range goFlags {
		name, _, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		if name != "tags" && name != "pgo" {
			kept = append(kept, flag)
		}
	}
	return kept
}

// sameBuildListAnswers reports whether other answers every name answer does but buildListUnread's, and the same.
func sameBuildListAnswers(answer map[string]string, other map[string]string) bool {
	for name, value := range answer {
		if buildListUnread[name] {
			continue
		}
		if otherValue, answered := other[name]; !answered || otherValue != value {
			return false
		}
	}
	return true
}
