package go_engine

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// invocation is one command line, read and checked.
type invocation struct {
	// root is the absolute directory of the go.work or the go.mod.
	root string

	// lintConfiguration is the settings file `--lint-config` named, absolute, or empty for the one beside the root.
	lintConfiguration string

	// switches holds every switch named, by name, with the value it was given.
	switches map[string]bool

	// paths narrow the scope, each absolute, as given.
	paths []string
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
