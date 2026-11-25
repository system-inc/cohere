package go_language

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// switchNames are the switches the front door forwards, each `--name` or `--name=true|false`.
//
// `no-cache` turns off the per-package findings cache's project store (see lintCache), as the front door's run cache
// is off for the same run, so a run under it computes every package from source but what the system cache holds.
//
// `no-system-cache` turns off the system cache (#syscache), the standard library's tables in the user's cache
// directory, which a run neither reads nor writes under it, so it computes the standard library. Beside `no-cache`,
// a run computes everything from scratch.
//
// `host-build-only` checks the host's build alone (#qqyq61s). Without it the run checks, beside the host's build, every
// other build the census needs (otherBuilds), and the project record names each file only such a build compiles as
// checked by it (#72n9mq0); with it each such file keeps its constraint's reason, a gap. cohere-go reads it through
// checksEveryBuild. The front door forwards it to cohere-go alone, and off changes nothing.
//
// `build-dependence` has cohere-go compute, for every extra build the run checks, which of the host's package-level
// declarations the build can change (#qkq4gx5), and write the counts on the build's record, observed only: it changes
// no finding. Under `host-build-only` it does nothing. cohere-go reads it alone for now, and the census runs the
// cohere-go directly, so the front door does not forward it.
//
// `timing` measures what the run's CPU goes to and writes it as the timing record (pipeline.timing, #3apgjer).
//
// `timeline` records when each phase of the run and each build began and ended, on one clock from the process's start,
// with the process's CPU at each, and writes it as the timeline record, the last before the summary (#3v2zp7g). It
// changes nothing of the run, unlike `timing`, which turns the cache off and runs the builds one at a time.
//
// `duplicates` asks for the duplicates report (#jfvhbj7): the lint pass hashes every function beside the rules, and
// one duplicates record after the unused phase's says what it grouped. Additive within contract 3, and cohere-go's
// alone: cohere-swift refuses it as a flag it does not know.
var switchNames = []string{
	"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
	"rules", "rules-enabled", "version", "no-cache", "no-system-cache", "host-build-only", "timing", "timeline",
	"duplicates", "build-dependence",
}

// notImplementedFlags are the contract's flags cohere-go does not do yet. Each is refused by name, because a
// flag accepted and ignored reads as a run that did what was asked.
var notImplementedFlags = []string{"unused", "unused-all", "unused-deep", "explain"}

// valuedFlags are the flags that take a value, as `--name value` or `--name=value`.
//
// `stdin-filepath` is the editor's save (#qtna8m4): the path of one file whose bytes are the buffer on standard input,
// for which the run prints on standard output what `--fix` would write, and writes nothing (pipeline.runStdin).
//
// `profile` is the file the process writes its CPU profile to (Main), which the front door names beside its own when
// its run is profiled, and release's training script names when it runs cohere-go directly (#r33n96f). It changes
// nothing of the run.
var valuedFlags = []string{"contract", "root", "lint-config", "stdin-filepath", "profile"}

// refusedTogether are the combinations the contract refuses, each switch with the switches it cannot be named
// beside. `--format-only` asks for formatting alone, so asking it for a phase or for no formatting asks two
// opposite things; `--no-format` leaves formatting out, so asking it for formatting does too.
var refusedTogether = []struct {
	name    string
	besides []string
}{
	{name: "format-only", besides: []string{"fix", "types", "lint", "no-format"}},
	{name: "no-format", besides: []string{"format", "format-all"}},
}

// mode is which question a run answers, and so which records it writes.
type mode string

const (
	modeCheck        mode = "Check"
	modeVersion      mode = "Version"
	modeRules        mode = "Rules"
	modeRulesEnabled mode = "RulesEnabled"

	// modeStdin is the editor's save, which prints one file's text rather than records.
	modeStdin mode = "Stdin"
)

// parseInvocation reads the command line, and refuses one cohere-go cannot honor in full.
//
// A contract mismatch is the refusal named whenever there is one, even beside a flag cohere-go does not
// know: a front door speaking another version may well send flags this one has never heard of, and the
// version is the reason.
func parseInvocation(arguments []string) (invocation, error) {
	request := invocation{switches: map[string]bool{}}
	contractValue := ""
	contractGiven := false
	var firstRefusal error
	refuseOnce := func(refusal error) {
		if firstRefusal == nil {
			firstRefusal = refusal
		}
	}

	pathsOnly := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if pathsOnly || !strings.HasPrefix(argument, "-") {
			request.paths = append(request.paths, argument)
			continue
		}
		if argument == "--" {
			pathsOnly = true
			continue
		}
		spelled, isLong := strings.CutPrefix(argument, "--")
		if !isLong {
			refuseOnce(fmt.Errorf("%s is not a flag cohere-go takes, whose flags are spelled with two dashes",
				argument))
			continue
		}
		name, value, hasValue := strings.Cut(spelled, "=")
		switch {
		case slices.Contains(valuedFlags, name):
			if !hasValue {
				if index+1 >= len(arguments) {
					refuseOnce(fmt.Errorf("--%s needs a value, and it was the last argument", name))
					continue
				}
				index++
				value = arguments[index]
			}
			switch name {
			case "contract":
				contractValue, contractGiven = value, true
			case "root":
				request.root = value
			case "lint-config":
				request.lintConfiguration = value
			case "stdin-filepath":
				// Clean, as the census names every file, so the one path matches the file it names.
				request.stdinPath = filepath.Clean(value)
			case "profile":
				// Refused rather than read as no profile, since a caller that asked for one would find none.
				if value == "" {
					refuseOnce(errors.New("--profile names no file to write the profile to"))
					continue
				}
				request.profilePath = value
			}
		case slices.Contains(switchNames, name):
			switch {
			case !hasValue || value == "true":
				request.switches[name] = true
			case value == "false":
				request.switches[name] = false
			default:
				refuseOnce(fmt.Errorf("--%s=%s: a switch takes true or false", name, value))
			}
		case slices.Contains(notImplementedFlags, name):
			refuseOnce(fmt.Errorf("--%s is not implemented for Go yet, so the run was refused rather than run "+
				"without it", name))
		default:
			refuseOnce(fmt.Errorf("--%s is not a flag cohere-go takes", name))
		}
	}

	if contractError := checkContract(contractValue, contractGiven); contractError != nil {
		return invocation{}, contractError
	}
	if firstRefusal != nil {
		return invocation{}, firstRefusal
	}
	if checkError := request.check(); checkError != nil {
		return invocation{}, checkError
	}
	request.resolveRoot()
	return request, nil
}

// checkContract refuses a command line that does not speak cohere-go's contract version.
func checkContract(value string, given bool) error {
	if !given {
		return fmt.Errorf("no --contract was given, so cohere-go cannot tell whether it speaks the front door's "+
			"version (it speaks contract %d)", contract.Version)
	}
	spoken, parseError := strconv.Atoi(value)
	if parseError != nil {
		return fmt.Errorf("--contract %q is not a version number (cohere-go speaks contract %d)", value,
			contract.Version)
	}
	if spoken != contract.Version {
		return fmt.Errorf("the front door speaks contract %d and cohere-go speaks contract %d", spoken,
			contract.Version)
	}
	return nil
}
