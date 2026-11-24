package go_engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/timeline"
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

	// profilePath is the file `--profile` names, where Main writes this process's CPU profile for `go tool pprof`, as
	// given, or empty for a run that writes none (#r33n96f). Main's rather than the run's, since a profile is the
	// whole process's, and a test runs the engine in process beside others.
	profilePath string

	// standardLibrary is where the run's standard-library entries come from and go (see lintCache), never read from
	// the command line.
	standardLibrary standardLibrarySource

	// answersInPart is check.Caching's AnswersInPart, the packages whose entries the run writes with only the answers
	// its questions computed, for a test of the reload such an entry makes, never read from the command line.
	answersInPart map[string]bool

	// timeline is the run's recorder under `--timeline`, made as the command line is read (newTimeline), nil for a run
	// that did not ask, never read from the command line.
	timeline *timeline.Recorder

	// timelineClock is the clock the run's recorder reads (newTimeline), the zero Clock for the system's unless a test
	// counts the run's reads of it, never read from the command line.
	timelineClock timeline.Clock

	// readFromDisk is how read reads a file from disk, os.ReadFile unless a test watches the run's reads, never read
	// from the command line.
	readFromDisk func(file string) ([]byte, error)

	// hostFetchHeld is a test's hold on the early start's own fetch of the host's lister state, called once the host's
	// go env is handed out and before that fetch (startEarly), so the test's fetch is the first, nil in every run but a
	// test's, never read from the command line.
	hostFetchHeld func()
}

// timelineRefusals are the runs that refuse `--timeline`, by mode, each with what it does in place of a check: only a
// check run has phases and builds to map, and a switch accepted and ignored reads as a run that did what was asked.
var timelineRefusals = map[mode]string{
	modeRules:        "lists the rules",
	modeRulesEnabled: "lists the rules the settings turn on",
	modeVersion:      "prints the version",
	modeStdin:        "is the editor's save, which prints one file's text",
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
	if refused, isRefused := timelineRefusals[request.mode()]; isRefused && request.switches["timeline"] {
		return fmt.Errorf("--timeline maps a check run's phases and builds, and this run %s, so it was refused "+
			"rather than run without it", refused)
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
// so `--fix` and no other phase and no paths beside it. The file is checked as the host builds it, since only the lint
// phase checks another build and a save asks for none, so `--host-build-only` says what a save already does. The front
// door refuses the same before it starts the engine (errorStdinNeedsFix); this is for a caller that does not.
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
	return nil
}

// checksEveryBuild reports whether the run checks, beside the host's build, every other build the census needs
// (otherBuilds): always, unless `--host-build-only` narrows it to the host's alone (#qqyq61s).
func (request invocation) checksEveryBuild() bool {
	return !request.switches["host-build-only"]
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

// judgesDependence reports whether the run computes each extra build's dependence (#qkq4gx5): `--build-dependence`
// in a run of every build, which alone has extra builds to judge. Such a run reads the findings cache and the system
// cache's standard-library entries as any other run does, its host and each extra build alike (#wzre5eg); the standard
// library the host restores is judged on a graph of its own, loaded from source beside the host's lint, and anything
// else restored fails wide (buildDependenceGraph, #ss4t5g6). Its rules wait for the load's end (lintOverlaps), since
// they let go of each package's syntax as they finish with it, and its extra builds restore nothing from the host's
// run (censusKeepsHostRestoreOff), for the facts census.
func (request invocation) judgesDependence() bool {
	return request.switches["build-dependence"] && request.checksEveryBuild()
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

// keysByIdentity reports whether the run may key anything by the engine's identity (cache.ExecutableIdentity), which a
// run hashes the whole executable for: one that loads its packages, for their types, their lint or a fix's long-line
// wrap, with a project store or a system cache to key them for. A run with neither, under `--no-cache` and
// `--no-system-cache` together, keys nothing, and so does one that only formats, which loads nothing. It may say yes
// of a run that ends up keying nothing, a system cache with no root or a fix that wraps no line, which only hashes
// beside the run for nothing; it never says no of one that keys.
func (request invocation) keysByIdentity() bool {
	if request.switches["no-cache"] && request.switches["no-system-cache"] || request.switches["format-only"] {
		return false
	}
	return request.requests(contract.PhaseTypes) || request.requests(contract.PhaseLint) ||
		request.requests(contract.PhaseFix)
}
