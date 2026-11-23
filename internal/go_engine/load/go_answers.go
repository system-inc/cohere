package load

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
)

// GoAnswers are what go env and `go tool dist list` answered a test process, kept by everything that decides each
// answer, so a run asking under what decided one asked before reads it rather than starting a child (#2htryg8). On
// darwin go starts each child with fork, which marks the whole heap copy-on-write, so a test process running hundreds
// of runs side by side faults on every page it next writes after each one; the engine's own run asks every time, since
// a person's environment and toolchain move between runs. Nil in every run but a test's (Children.Answers); a failed
// ask is never kept.
type GoAnswers struct {
	lock sync.Mutex

	// environments are go env's answers by goEnvironmentKey, each with the workspace it was asked in, which the answer
	// is moved from to the asker's (relocated).
	environments map[string]keptGoEnvironment

	// ports are what `go tool dist list -json` printed, by the key its asker gave (Ports).
	ports map[string][]byte
}

// keptGoEnvironment is one go env answer GoAnswers keeps, and the workspace it was asked in.
type keptGoEnvironment struct {
	workspace string
	answer    map[string]string
}

// NewGoAnswers is a GoAnswers that has kept nothing yet.
func NewGoAnswers() *GoAnswers {
	return &GoAnswers{environments: map[string]keptGoEnvironment{}, ports: map[string][]byte{}}
}

// Ports is what `go tool dist list -json` printed under key, which the caller makes of everything that decides it,
// asked by ask the first time and kept when it succeeds.
func (answers *GoAnswers) Ports(key string, ask func() ([]byte, error)) ([]byte, error) {
	answers.lock.Lock()
	output, kept := answers.ports[key]
	answers.lock.Unlock()
	if kept {
		return output, nil
	}
	output, askError := ask()
	if askError != nil {
		return nil, askError
	}
	answers.lock.Lock()
	defer answers.lock.Unlock()
	answers.ports[key] = output
	return output, nil
}

// goEnvironment is go env's answer in workspace under environment, the whole one, read from an answer kept under the
// same key, moved to workspace (relocated), or asked by ask and kept when it succeeds. Each call returns a map of its
// own, since a test's hook changes the answer it is given (HookGoEnvironmentAnswers).
func (answers *GoAnswers) goEnvironment(
	workspace string,
	environment []string,
	ask func() (map[string]string, error),
) (map[string]string, error) {
	key, keyError := goEnvironmentKey(workspace, environment)
	if keyError != nil {
		// Asked, and kept by no key: a workspace whose go.mod or go.work cannot be read is one go env reports on itself.
		return ask()
	}
	answers.lock.Lock()
	kept, found := answers.environments[key]
	answers.lock.Unlock()
	if found {
		return relocated(kept.answer, kept.workspace, workspace), nil
	}
	answer, askError := ask()
	if askError != nil {
		return nil, askError
	}
	answers.lock.Lock()
	defer answers.lock.Unlock()
	answers.environments[key] = keptGoEnvironment{workspace: workspace, answer: maps.Clone(answer)}
	return answer, nil
}

// goEnvironmentKey is everything go env's answer in workspace under environment, the whole one, turns on but where the
// workspace is: the environment but `PWD`, which askGoEnvironment sets to workspace, in its order, since a later entry
// of a name wins; and the bytes of the go.work and the go.mod go finds there, and of each module's go.mod the go.work
// uses, which `GOTOOLCHAIN`, `GODEBUG`, `GOMOD` and `GOWORK` are read from. Each such file is named by its path below
// workspace, so the answer moves to any workspace holding the same files at the same paths. A file go would find above
// the workspace, or a module the go.work uses outside it, is named by its absolute path, so its answer moves nowhere.
// The environment must turn off the person's go env file (`GOENV=off`, as TestMain sets), and name no go.work of its
// own (`GOWORK`) and no -modfile in `GOFLAGS`, files elsewhere whose bytes the key does not hold; any other environment
// gets an error, and is asked every time. `GOROOT`'s go.env is the toolchain's, which the environment's `PATH` and
// `GOROOT` pick.
func goEnvironmentKey(workspace string, environment []string) (string, error) {
	var key strings.Builder
	// The last entry of a name is the one go reads.
	variables := map[string]string{}
	for _, variable := range environment {
		if strings.HasPrefix(variable, "PWD=") {
			continue
		}
		if name, value, found := strings.Cut(variable, "="); found {
			variables[name] = value
		}
		key.WriteString(variable)
		key.WriteByte(0)
	}
	if variables["GOENV"] != "off" {
		return "", errors.New("the environment reads a go env file, whose bytes no key holds")
	}
	if goWork := variables["GOWORK"]; goWork != "" && goWork != "off" {
		return "", errors.New("the environment names a go.work, whose bytes no key holds")
	}
	if strings.Contains(variables["GOFLAGS"], "-modfile") {
		return "", errors.New("the go flags name a go.mod of their own, whose bytes no key holds")
	}
	goWork, goWorkError := keyFile(&key, workspace, "go.work")
	if goWorkError != nil {
		return "", goWorkError
	}
	if _, goModError := keyFile(&key, workspace, "go.mod"); goModError != nil {
		return "", goModError
	}
	if goWork == nil {
		return key.String(), nil
	}
	work, parseError := modfile.ParseWork("go.work", goWork, nil)
	if parseError != nil {
		return "", parseError
	}
	for _, use := range work.Use {
		directory := use.Path
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(workspace, directory)
		}
		used, readError := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readError != nil && !errors.Is(readError, fs.ErrNotExist) {
			return "", readError
		}
		key.WriteString("use\x00" + keyPath(workspace, directory) + "\x00")
		key.Write(used)
		key.WriteByte(0)
	}
	return key.String(), nil
}

// keyFile writes to key the file named name go finds in workspace, the nearest at or above it, by its path
// (keyPath) and bytes, or that it finds none, and returns its bytes, nil for none.
func keyFile(key *strings.Builder, workspace string, name string) ([]byte, error) {
	for directory := workspace; ; directory = filepath.Dir(directory) {
		contents, readError := os.ReadFile(filepath.Join(directory, name))
		if readError == nil {
			key.WriteString(name + "\x00" + keyPath(workspace, directory) + "\x00")
			key.Write(contents)
			key.WriteByte(0)
			return contents, nil
		}
		if !errors.Is(readError, fs.ErrNotExist) {
			return nil, readError
		}
		if filepath.Dir(directory) == directory {
			key.WriteString(name + "\x00none\x00")
			return nil, nil
		}
	}
}

// keyPath is directory as a key names it: its path below workspace, or its absolute path outside it.
func keyPath(workspace string, directory string) string {
	relative, relativeError := filepath.Rel(workspace, directory)
	if relativeError != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "absolute " + directory
	}
	return "below " + filepath.ToSlash(relative)
}

// relocated is answer, asked in from, as go env answers in to: each value that is from or a path below it, as `GOMOD`
// and `GOWORK` are, names the same path below to. A copy, so the kept answer is never changed.
func relocated(answer map[string]string, from string, to string) map[string]string {
	moved := make(map[string]string, len(answer))
	for name, value := range answer {
		if value == from {
			value = to
		} else if below, isBelow := strings.CutPrefix(value, from+string(filepath.Separator)); isBelow {
			value = filepath.Join(to, below)
		}
		moved[name] = value
	}
	return moved
}
