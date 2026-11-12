package go_engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// invocation is one command line, read and checked.
type invocation struct {
	// root is the absolute directory of the go.work or the go.mod, as its real path once the command line is read
	// (resolveRoot).
	root string

	// lintConfiguration is the settings file `--lint-config` named, absolute, or empty for the one beside the root.
	lintConfiguration string

	// switches holds every switch named, by name, with the value it was given.
	switches map[string]bool

	// paths narrow the scope, each absolute, as given.
	paths []string

	// targets are the settings' `targets`, each `GOOS/GOARCH`, which build selection checks beside the platforms the
	// files name (#ntamp9e), set once the settings are read.
	targets []string

	// stdinPath is the file `--stdin-filepath` names, absolute and clean, or empty for a run over the tree. buffer is
	// its bytes as the editor holds them, read from standard input, never from the command line, which stand in for the
	// file's on disk everywhere the run reads it (read).
	stdinPath string
	buffer    []byte

	// standardLibrary is the standard library's entries the run may read from the binary (see lintCache), never
	// read from the command line.
	standardLibrary builtInStandardLibrary

	// answersInPart is check.Caching's AnswersInPart, the packages whose entries the run writes with only the answers
	// its questions computed, for a test of the reload such an entry makes, never read from the command line.
	answersInPart map[string]bool

	// readFromDisk is how read reads a file from disk, os.ReadFile unless a test watches the run's reads, never read
	// from the command line.
	readFromDisk func(file string) ([]byte, error)
}

// check refuses what each flag allows alone but the command line does not allow as a whole.
func (request invocation) check() error {
	if request.root == "" {
		return errors.New("no --root was given, so there is no project to check")
	}
	if !filepath.IsAbs(request.root) {
		return fmt.Errorf("--root %s is not an absolute path", request.root)
	}
	if request.lintConfiguration != "" && !filepath.IsAbs(request.lintConfiguration) {
		return fmt.Errorf("--lint-config %s is not an absolute path", request.lintConfiguration)
	}
	for _, path := range request.paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s is not an absolute path, and the front door sends every path absolute", path)
		}
	}
	if stdinError := request.checkStdin(); stdinError != nil {
		return stdinError
	}
	for _, refused := range refusedTogether {
		if !request.switches[refused.name] {
			continue
		}
		for _, beside := range refused.besides {
			if request.switches[beside] {
				return fmt.Errorf("--%s cannot be named with --%s, since the two ask for opposite runs",
					refused.name, beside)
			}
		}
	}
	return nil
}

// resolveRoot makes the root its real path, every symbolic link on the way resolved, and moves each path the command
// line named under the root as given, and the save's file, to the same place under the real one (#yyv7nr2). Every
// path the run compares with the root is then of one form: the census walks the root, which a walk enters only
// as a directory and never as a link to one, so a root given as a link held no file, and go, run from the real
// root, names real paths. A root that does not resolve is left as given, for describeProject to refuse in its own
// words; a path outside the root as given is left as given too, for the scope to refuse.
func (request *invocation) resolveRoot() {
	resolved, resolveError := filepath.EvalSymlinks(request.root)
	if resolveError != nil || resolved == request.root {
		return
	}
	given := filepath.Clean(request.root)
	beneath := func(path string) string {
		if relative, inside := strings.CutPrefix(path, given); inside &&
			(relative == "" || strings.HasPrefix(relative, string(filepath.Separator))) {
			return resolved + relative
		}
		return path
	}
	for index, path := range request.paths {
		request.paths[index] = beneath(filepath.Clean(path))
	}
	if request.stdinPath != "" {
		// Cleaned as each path is, though parseInvocation stores it clean, so the prefix test never rests on that.
		request.stdinPath = beneath(filepath.Clean(request.stdinPath))
	}
	request.root = resolved
}

// checkStdin refuses a save's command line that asks more than a save answers: what `--fix` writes for the one file,
// so `--fix` and no other phase, no paths beside it, and no other build, since the file is checked as the host builds
// it. The front door refuses the same before it starts the engine (errorStdinNeedsFix); this is for a caller that
// does not.
func (request invocation) checkStdin() error {
	if request.stdinPath == "" {
		return nil
	}
	if !filepath.IsAbs(request.stdinPath) {
		return fmt.Errorf("--stdin-filepath %s is not an absolute path", request.stdinPath)
	}
	if !request.switches["fix"] || request.switches["no-fix"] || request.switches["types"] ||
		request.switches["lint"] || request.switches["format-only"] || len(request.paths) > 0 {
		return errors.New("--stdin-filepath answers what --fix writes for one file, so it takes --fix, no other " +
			"phase and no other path")
	}
	if request.switches["other-builds"] {
		return errors.New("--stdin-filepath answers for one file as the host builds it, so it takes no " +
			"--other-builds")
	}
	return nil
}

// mode is the question the command line asks, by the precedence the front door gives the same switches.
func (request invocation) mode() mode {
	switch {
	case request.switches["rules"]:
		return modeRules
	case request.switches["rules-enabled"]:
		return modeRulesEnabled
	case request.switches["version"]:
		return modeVersion
	case request.stdinPath != "":
		return modeStdin
	}
	return modeCheck
}

// read is a file's bytes as the run takes them: the buffer for the file a save names, in place of the disk's, and the
// disk's for every other. Every reader of the run's files reads through what describeProject read here (held), so the
// buffer is the file for gofmt, the load, the findings cache's keys, the rules, suppression and the rename's names.
func (request invocation) read(file string) ([]byte, error) {
	if request.stdinPath != "" && file == request.stdinPath {
		return request.buffer, nil
	}
	if request.readFromDisk != nil {
		return request.readFromDisk(file)
	}
	return os.ReadFile(file)
}

// requests reports whether the command line asks for a phase, read as the front door reads it: no phase switch
// asks for fix, types and lint; `--fix`, `--types` or `--lint` asks for only the ones named; `--format-only`
// asks for fix alone. Nothing asks for unused, which is refused until it is implemented.
func (request invocation) requests(phase contract.PhaseName) bool {
	if request.switches["format-only"] {
		return phase == contract.PhaseFix
	}
	anyNamed := request.switches["fix"] || request.switches["types"] || request.switches["lint"]
	switch phase {
	case contract.PhaseFix:
		return request.switches["fix"] || !anyNamed
	case contract.PhaseTypes:
		return request.switches["types"] || !anyNamed
	case contract.PhaseLint:
		return request.switches["lint"] || !anyNamed
	}
	return false
}
