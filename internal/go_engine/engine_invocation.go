package go_engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// switchNames are the switches the front door forwards, each `--name` or `--name=true|false`.
//
// `no-cache` turns off the per-package findings cache (see lintCache), as the front door's run cache is off for the
// same run, so a run under it computes every package from source.
//
// `other-builds` checks, beside the host's build, every other build the census needs (otherBuilds), and the project
// record names each file only such a build compiles as checked by it (#72n9mq0). The front door forwards it to this
// engine alone, and off changes nothing.
//
// `build-dependence` has the engine compute, for every extra build `other-builds` checks, which of the host's
// package-level declarations the build can change (#qkq4gx5), and write the counts on the build's record, observed
// only: it changes no finding. Without `other-builds` it does nothing. The engine reads it alone for now, and the
// census runs the engine directly, so the front door does not forward it.
//
// `timing` measures what the run's CPU goes to and writes it as the timing record (pipeline.timing, #3apgjer).
//
// `duplicates` asks for the duplicates report (#jfvhbj7): the lint pass hashes every function beside the rules, and
// one duplicates record after the unused phase's says what it grouped. Additive within contract 3, and this engine's
// alone: the Swift engine refuses it as a flag it does not know.
var switchNames = []string{
	"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
	"rules", "rules-enabled", "version", "no-cache", "other-builds", "timing", "duplicates", "build-dependence",
}

// notImplementedFlags are the contract's flags this engine does not do yet. Each is refused by name, because a
// flag accepted and ignored reads as a run that did what was asked.
var notImplementedFlags = []string{"unused", "unused-all", "unused-deep", "explain"}

// valuedFlags are the flags that take a value, as `--name value` or `--name=value`.
//
// `stdin-filepath` is the editor's save (#qtna8m4): the path of one file whose bytes are the buffer on standard input,
// for which the run prints on standard output what `--fix` would write, and writes nothing (pipeline.runStdin).
var valuedFlags = []string{"contract", "root", "lint-config", "stdin-filepath"}

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

// parseInvocation reads the command line, and refuses one the engine cannot honor in full.
//
// A contract mismatch is the refusal named whenever there is one, even beside a flag this engine does not
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
			refuseOnce(fmt.Errorf("%s is not a flag the Go engine takes, whose flags are spelled with two dashes",
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
			refuseOnce(fmt.Errorf("--%s is not a flag the Go engine takes", name))
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

// checkContract refuses a command line that does not speak this engine's contract version.
func checkContract(value string, given bool) error {
	if !given {
		return fmt.Errorf("no --contract was given, so the engine cannot tell whether it speaks the front door's "+
			"version (it speaks contract %d)", contract.Version)
	}
	spoken, parseError := strconv.Atoi(value)
	if parseError != nil {
		return fmt.Errorf("--contract %q is not a version number (this engine speaks contract %d)", value,
			contract.Version)
	}
	if spoken != contract.Version {
		return fmt.Errorf("the front door speaks contract %d and this engine speaks contract %d", spoken,
			contract.Version)
	}
	return nil
}
