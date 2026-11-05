package main

import (
	"fmt"
	"maps"
	"slices"
)

// engineFlags is which of the front door's flags an engine run takes, and how. A flag in neither list is
// refused by name, because a flag accepted and ignored reads as a run that did what was asked.
type engineFlags struct {
	// PassThroughSwitches are the boolean flags the engine reads with the meaning the contract gives them,
	// forwarded only when the caller typed them.
	PassThroughSwitches []string

	// FrontDoorFlags are the flags the front door answers for the run itself: where the project is, how
	// the report is printed, the profile of this process, and the lint config (and fix passes, where the
	// engine takes them) it forwards with their values.
	FrontDoorFlags []string
}

// engineArguments builds an engine's command line, `--contract 3 --root <root> [flags] [paths]`, and says
// which question the run answers.
//
// Every path is made absolute against where the caller typed it, so the engine never has to know what
// `--directory` meant. Takes the flag reader as a parameter so a test can state the flags it means without
// touching the process's own.
func engineArguments(
	language engineLanguage,
	flags engineFlags,
	location projectLocation,
	given map[string]bool,
	value func(string) string,
	positionals []string,
) ([]string, engineMode, error) {
	for _, refused := range []string{"explain"} {
		if given[refused] {
			// Refused here rather than forwarded for the engine to refuse, because its argument is a path and
			// the engine's sentence would name the path rather than the flag.
			return nil, "", fmt.Errorf("--%s is not implemented for %s yet, so nothing was checked",
				refused, language.Name)
		}
	}
	// Sorted, so a command line naming two of them is refused for the same one every time.
	for _, name := range slices.Sorted(maps.Keys(given)) {
		// A switch turned off asks for nothing, so there is nothing to honor or refuse.
		if slices.Contains(flags.PassThroughSwitches, name) || slices.Contains(flags.FrontDoorFlags, name) ||
			name == "tsconfig" || value(name) == "false" {
			continue
		}
		return nil, "", fmt.Errorf("--%s is not implemented for %s yet, so the run was refused rather than run "+
			"without it, and nothing was checked", name, language.Name)
	}

	arguments := []string{"--contract", fmt.Sprint(engineContractVersion), "--root", location.Root}
	for _, name := range flags.PassThroughSwitches {
		if given[name] && value(name) == "true" {
			arguments = append(arguments, "--"+name)
		}
	}
	if given["lint-config"] {
		arguments = append(arguments, "--lint-config", location.LintConfigFileName)
	}
	if given["fix-passes"] {
		arguments = append(arguments, "--fix-passes", value("fix-passes"))
	}
	for _, positional := range positionals {
		arguments = append(arguments, absoluteFrom(location.ArgumentBase, positional))
	}

	// The same precedence the TypeScript run gives these, so one command line asks one question whichever
	// engine answers it.
	mode := engineModeCheck
	switch {
	case given["rules"] && value("rules") == "true":
		mode = engineModeRules
	case given["rules-enabled"] && value("rules-enabled") == "true":
		mode = engineModeRulesEnabled
	case given["version"] && value("version") == "true":
		mode = engineModeVersion
	}
	return arguments, mode, nil
}
