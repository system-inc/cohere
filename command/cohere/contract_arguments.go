package main

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// contractFlags is which of the front door's flags a child run takes, and how. A flag in neither list is
// refused by name, because a flag accepted and ignored reads as a run that did what was asked.
type contractFlags struct {
	// PassThroughSwitches are the boolean flags the child reads with the meaning the contract gives them,
	// forwarded only when the caller typed them.
	PassThroughSwitches []string

	// FrontDoorFlags are the flags the front door answers for the run itself: where the project is, how
	// the report is printed, the profile of this process, and the lint config (and fix passes and the
	// editor's save's path, where the child takes them) it forwards with their values.
	FrontDoorFlags []string

	// ProfileName names the child's own CPU profile when the run is given `--profile <file>`: the front door profiles
	// its own process to that file and forwards `--profile` to the child with the name inserted before the file's
	// extension (contractProfilePath), so each process writes a file of its own beside the other. Empty for a child
	// that takes no `--profile`, whose run says so and writes no file for it (#r33n96f).
	ProfileName string
}

// contractProfilePath is the file a child child writes its CPU profile to when the front door's run profiles itself
// to frontDoorProfile: the same directory and name, with the child's name before the extension, so `run.pprof`
// gives `run.go.pprof`, and a name with no extension gains one (`run` gives `run.go`). Absolute against
// workingDirectory, where the front door's own profile is created, so the two land side by side.
func contractProfilePath(workingDirectory string, frontDoorProfile string, profileName string) string {
	path := absoluteFrom(workingDirectory, frontDoorProfile)
	extension := filepath.Ext(path)
	return strings.TrimSuffix(path, extension) + "." + profileName + extension
}

// contractArguments builds a child's command line, `--contract 3 --root <root> [flags] [paths]`, and says
// which question the run answers.
//
// Every path is made absolute against where the caller typed it, so the child never has to know what
// `--directory` meant. Takes the flag reader as a parameter so a test can state the flags it means without
// touching the process's own.
func contractArguments(
	language contractLanguage,
	flags contractFlags,
	location projectLocation,
	given map[string]bool,
	value func(string) string,
	positionals []string,
) ([]string, contractMode, error) {
	for _, refused := range []string{"explain"} {
		if given[refused] {
			// Refused here rather than forwarded for the child to refuse, because its argument is a path and
			// the child's sentence would name the path rather than the flag.
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

	arguments := []string{"--contract", fmt.Sprint(contractVersion), "--root", location.Root}
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
	if given["stdin-filepath"] {
		arguments = append(arguments, "--stdin-filepath", absoluteFrom(location.ArgumentBase, value("stdin-filepath")))
	}
	// Only a profile asked for: a run without one keeps the command line it always had, which nothing a replay keys on
	// reads anyway, since the run cache never admits `--profile` (runCacheAdmits).
	if given["profile"] && value("profile") != "" && flags.ProfileName != "" {
		arguments = append(arguments, "--profile",
			contractProfilePath(location.WorkingDirectory, value("profile"), flags.ProfileName))
	}
	for _, positional := range positionals {
		arguments = append(arguments, absoluteFrom(location.ArgumentBase, positional))
	}

	// The same precedence the TypeScript run gives these, so one command line asks one question whichever
	// child answers it.
	mode := contractModeCheck
	switch {
	case given["rules"] && value("rules") == "true":
		mode = contractModeRules
	case given["rules-enabled"] && value("rules-enabled") == "true":
		mode = contractModeRulesEnabled
	case given["version"] && value("version") == "true":
		mode = contractModeVersion
	}
	return arguments, mode, nil
}
