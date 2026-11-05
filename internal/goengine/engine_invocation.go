package goengine

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/goengine/contract"
)

// switchNames are the switches the front door forwards, each `--name` or `--name=true|false`.
var switchNames = []string{
	"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
	"rules", "rules-enabled", "version",
}

// notImplementedFlags are the contract's flags this engine does not do yet. Each is refused by name, because a
// flag accepted and ignored reads as a run that did what was asked.
var notImplementedFlags = []string{"unused", "unused-all", "unused-deep", "timing", "explain"}

// valuedFlags are the flags that take a value, as `--name value` or `--name=value`.
var valuedFlags = []string{"contract", "root", "lint-config"}

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
)

// invocation is one command line, read and checked.
type invocation struct {
	// root is the absolute directory of the go.work or the go.mod.
	root string

	// lintConfig is the settings file `--lint-config` named, absolute, or empty for the one beside the root.
	lintConfig string

	// switches holds every switch named, by name, with the value it was given.
	switches map[string]bool

	// paths narrow the scope, each absolute, as given.
	paths []string
}

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
				request.lintConfig = value
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

// check refuses what each flag allows alone but the command line does not allow as a whole.
func (request invocation) check() error {
	if request.root == "" {
		return errors.New("no --root was given, so there is no project to check")
	}
	if !filepath.IsAbs(request.root) {
		return fmt.Errorf("--root %s is not an absolute path", request.root)
	}
	if request.lintConfig != "" && !filepath.IsAbs(request.lintConfig) {
		return fmt.Errorf("--lint-config %s is not an absolute path", request.lintConfig)
	}
	for _, path := range request.paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s is not an absolute path, and the front door sends every path absolute", path)
		}
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

// mode is the question the command line asks, by the precedence the front door gives the same switches.
func (request invocation) mode() mode {
	switch {
	case request.switches["rules"]:
		return modeRules
	case request.switches["rules-enabled"]:
		return modeRulesEnabled
	case request.switches["version"]:
		return modeVersion
	}
	return modeCheck
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
