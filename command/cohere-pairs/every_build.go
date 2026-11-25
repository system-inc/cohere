package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// hostBuildOnlyUnknown is what a cohere from before every build was the default (#qqyq61s) says of
// `--host-build-only`, a switch it does not take, as parseInvocation words its refusal.
const hostBuildOnlyUnknown = "--host-build-only is not a flag cohere-go takes"

// everyBuildArguments are the arguments a side's Go child is given so that it checks every build the corpus's files
// name, read from the binary once, as it is built. A pair whose base is from before the flip (#qqyq61s) has a base that
// checks the host's build alone unless asked with `--other-builds`, and a branch that refuses that switch, so one
// command line for both sides would time the host's build against every build.
//
// The probe asks the Go child its version beside `--host-build-only`: `--version` writes one record and checks nothing,
// and the Go child reads every switch before it answers, refusing one it does not know with exit 2 before anything
// runs. Exit 0 is a cohere that takes the switch, so every build is its default and it is given nothing; the refusal of
// that switch by name is a cohere from before the flip, given `--other-builds`. Anything else refuses the pair,
// naming the binary, rather than guess which builds it would check. root is an absolute directory for `--root`, which
// the Go child requires and the version never reads.
func everyBuildArguments(binaryPath string, root string, environment []string) ([]string, error) {
	command := exec.Command(binaryPath, "go-language", "--contract", "3", "--root", root, "--version",
		"--host-build-only")
	command.Env = append(environmentWithout(environment, "GOFLAGS"), "COHERE_GO_CHILD=1")
	var standardError strings.Builder
	command.Stderr = &standardError
	runError := command.Run()
	if runError == nil {
		return nil, nil
	}
	if exitError, exited := errors.AsType[*exec.ExitError](runError); exited && exitError.ExitCode() == 2 &&
		strings.Contains(standardError.String(), hostBuildOnlyUnknown) {
		return []string{"--other-builds"}, nil
	}
	return nil, fmt.Errorf("%s go-language --version --host-build-only neither answered nor refused the switch by "+
		"name, so which builds it checks is unknown (%w): %s", binaryPath, runError,
		strings.TrimSpace(standardError.String()))
}

// describeEveryBuildArguments is what a side was given to check every build, for the table's header.
func describeEveryBuildArguments(binary builtBinary) string {
	if len(binary.everyBuildArguments) == 0 {
		return "nothing"
	}
	return "`" + strings.Join(binary.everyBuildArguments, " ") + "`"
}
