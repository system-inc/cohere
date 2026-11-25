package load

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// contextFormatVersion is hashed into every build context's key, so an answer kept in another shape is never read.
const contextFormatVersion = "go-list-context-1"

// contextKey is where the build context go list prints (contextTemplate) is kept in the system cache, for a command
// run under given, the environment as given before its `GOFLAGS` is made whole, with goFlags the whole `GOFLAGS`,
// cgoEnabled the `CGO_ENABLED` go reads, and answer go env's answer naming the toolchain, and false when none is kept
// (#sfjj2rh). Every printed line is a fact of the toolchain and the environment, never of the project, but for the
// toolchain the project's go and toolchain lines choose, which the answer's `GOROOT` and `GOVERSION` name. In order,
// each part length-prefixed:
//   - a marker naming the kind, the format version and the template, so another template keys apart;
//   - the flags, but those that change nothing go does (VerdictFlags): the last -tags is BuildTags, and -race, -msan
//     and -asan add their name to ToolTags; every other flag stays, so one that could change a line misses;
//   - cgoEnabled, CgoEnabled, which go reads from the environment or the go env file and otherwise derives from
//     whether `PATH` finds the default C compiler, so the key holds what go reports rather than `PATH`;
//   - the answer's `GOROOT`, `GOVERSION` and `GOEXPERIMENT`, the toolchain that answers (`GOROOT`, ReleaseTags,
//     Compiler) and its experiments, which ToolTags holds;
//   - every `GO*` and `CGO_*` variable of given, and `CC`, by name and value, sorted, `GOFLAGS` but as flags: the
//     target, `CGO_ENABLED` and the architecture levels as given, which go's architecture tags read from the process
//     alone (internal/buildcfg), never from the go env file, `GOMIPS`, `GOPPC64`, `GORISCV64` and `GOWASM` among them;
//   - the bytes of the go env file the answer names (`GOENV`) and of the toolchain's `go.env`, either of which can set
//     the target, an architecture level or an experiment with no variable in the process;
//   - the go `PATH` finds, the one the command starts, and the toolchain's own `bin/go`, each resolved through its
//     links, with its size and modification time, so a toolchain rebuilt in place keys apart.
//
// `HOME` is not in it: go reads it only for where the go env file, the build cache and `GOPATH` default to, the
// template prints none of those paths, and the go env file's bytes are in the key. None is kept under an overlay, which
// can replace a file go reads, nor with `GOFIPS140` on, which adds a build tag and the lister doubts anyway.
func contextKey(given []string, goFlags []string, cgoEnabled string, answer map[string]string) ([sha256.Size]byte,
	bool) {
	goRoot := answer["GOROOT"]
	if fips140 := answer["GOFIPS140"]; goRoot == "" || fips140 != "" && fips140 != "off" {
		return [sha256.Size]byte{}, false
	}
	for _, flag := range goFlags {
		if name, value, _ := strings.Cut(strings.TrimLeft(flag, "-"), "="); name == "overlay" && value != "" {
			return [sha256.Size]byte{}, false
		}
	}
	digest := sha256.New()
	writeParts(digest, []string{"go list context", contextFormatVersion, contextTemplate})
	writeParts(digest, []string{"GOFLAGS", VerdictFlags(strings.Join(goFlags, " "))})
	writeParts(digest, []string{"CGO_ENABLED", cgoEnabled})
	for _, name := range []string{"GOROOT", "GOVERSION", "GOEXPERIMENT"} {
		writeParts(digest, []string{name, answer[name]})
	}
	variables := contextVariables(given)
	writeParts(digest, []string{strconv.Itoa(len(variables))})
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		writeParts(digest, []string{name, variables[name]})
	}
	for _, file := range []string{answer["GOENV"], filepath.Join(goRoot, "go.env")} {
		contents, read := contextFile(file)
		if !read {
			return [sha256.Size]byte{}, false
		}
		writeParts(digest, []string{contents})
	}
	pathGo, lookError := exec.LookPath("go")
	if lookError != nil {
		return [sha256.Size]byte{}, false
	}
	for _, executable := range []string{pathGo, filepath.Join(goRoot, "bin", "go")} {
		writeParts(digest, []string{contextExecutable(executable)})
	}
	return [sha256.Size]byte(digest.Sum(nil)), true
}

// contextVariables are given's `GO*` and `CGO_*` variables and `CC`, by name, the last of a name as exec keeps it,
// `GOFLAGS` left out, which contextKey holds as flags.
func contextVariables(given []string) map[string]string {
	variables := map[string]string{}
	for _, variable := range given {
		name, value, found := strings.Cut(variable, "=")
		if !found || name == "GOFLAGS" {
			continue
		}
		if strings.HasPrefix(name, "GO") || strings.HasPrefix(name, "CGO_") || name == "CC" {
			variables[name] = value
		}
	}
	return variables
}

// contextFile is file's bytes as contextKey holds them, a word for none when file is not named, is `off` or is not
// there, and false when it could not be read.
func contextFile(file string) (string, bool) {
	if file == "" || file == "off" {
		return "no file", true
	}
	contents, readError := os.ReadFile(file)
	if errors.Is(readError, fs.ErrNotExist) {
		return "absent", true
	}
	if readError != nil {
		return "", false
	}
	return "file " + string(contents), true
}

// contextExecutable is the go at executable as contextKey holds it: its path resolved through its links, its size
// and its modification time, or a word for one not there.
func contextExecutable(executable string) string {
	absolute, absoluteError := filepath.Abs(executable)
	if absoluteError != nil {
		return "unresolved " + executable
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		return "absent " + absolute
	}
	information, statError := os.Stat(resolved)
	if statError != nil {
		return "absent " + resolved
	}
	return resolved + " " + strconv.FormatInt(information.Size(), 10) + " " +
		strconv.FormatInt(information.ModTime().UnixNano(), 10)
}

// predictedContextKey is contextKey for a go command under environment, the caller's entries over the process's own,
// predicted before go env answers there from host's answer, the run's host's go env: the whole `GOFLAGS` is
// environment's own with -pgo=off last, as ReadGoEnvironment makes it, or host's when environment sets none, and
// `CGO_ENABLED` environment's when it sets one, as an extra build always does, or host's. The answer's own key
// (ownContextKey) checks it.
func predictedContextKey(host *GoEnvironment, environment []string) ([sha256.Size]byte, bool) {
	given, variables := givenVariables(environment)
	goFlags := strings.Fields(host.answer["GOFLAGS"])
	if value := variables["GOFLAGS"]; value != "" {
		goFlags = strings.Fields(value)
	}
	cgoEnabled := host.answer["CGO_ENABLED"]
	if value := variables["CGO_ENABLED"]; value == "0" || value == "1" {
		cgoEnabled = value
	}
	return contextKey(given, append(goFlags, "-pgo=off"), cgoEnabled, host.answer)
}

// givenVariables is the environment a go command under environment is given, the caller's entries over the process's
// own, and its variables by name, the last of a name as exec keeps it.
func givenVariables(environment []string) ([]string, map[string]string) {
	given := append(os.Environ(), environment...)
	variables := map[string]string{}
	for _, variable := range given {
		if name, value, found := strings.Cut(variable, "="); found {
			variables[name] = value
		}
	}
	return given, variables
}

// ownContextKey is contextKey for go commands under goEnvironment, from its own answer.
func ownContextKey(goEnvironment *GoEnvironment) ([sha256.Size]byte, bool) {
	return contextKey(append(os.Environ(), goEnvironment.environment...), goEnvironment.reported.goFlags,
		goEnvironment.answer["CGO_ENABLED"], goEnvironment.answer)
}

// KeepContext has every lister under the answer read its build context, go list's eight lines (contextTemplate), from
// the system cache, where a run kept it under the key predicted from the host's go env (PredictContext), in place of
// running the command, and keep it there when it runs it and it succeeds, and reports whether it does (#sfjj2rh): a
// warm run's extra build then forks no go list. The answer's own key checks the prediction (keptContextKey), so one it
// contradicts runs the command. predicted is nil, and nothing is kept, under `--no-system-cache`, with no system cache,
// or under `--timing`. Called once the answer is read and before anything fetches its lister state.
func (goEnvironment *GoEnvironment) KeepContext(predicted *PredictedContext) bool {
	if predicted == nil {
		return false
	}
	goEnvironment.listerState.predictedContext = predicted
	return true
}
