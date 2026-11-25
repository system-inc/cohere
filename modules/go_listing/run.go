package go_listing

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// A Request is one go list run's flags and patterns, each flag by name, for Arguments to spell.
type Request struct {
	// Errors is -e: a package that can't be loaded is listed with its Error rather than failing the run.
	Errors bool
	// Dependencies is -deps: every package the patterns import, directly or not, listed before them.
	Dependencies bool
	// Tests is -test: each package's test variants and test main too.
	Tests bool
	// Compiled is -compiled: CompiledGoFiles, which runs cgo.
	Compiled bool
	// Export is -export: Export, which compiles.
	Export bool
	// WithoutVersionControl is -buildvcs=false, so go reads no version control state for a main
	// package. It changes nothing a Package decodes but its stamp, and saves the reads.
	WithoutVersionControl bool
	// Fields are the fields -json prints, "-json=A,B". None prints them all.
	Fields []string
	// BuildFlags are passed through as they are, in order, after the flags above: -tags, -mod,
	// -buildmode, -overlay, -trimpath, -pgo and the like.
	BuildFlags []string
	// Patterns are the packages to list, after every flag. None is the package in the directory go
	// runs in.
	Patterns []string
}

// Arguments are go's arguments for request: "list", its flags in a fixed order, -json, its build
// flags, then its patterns. There is no "--" before the patterns, since a runner's stand-in go refuses
// any flag it doesn't know, that one too. So a pattern that begins with "-", which go would read as a
// flag, is refused, as is a field with a comma in it, which would read as two.
func Arguments(request Request) ([]string, error) {
	arguments := []string{"list"}
	for _, flag := range []struct {
		asked bool
		flag  string
	}{
		{request.Errors, "-e"},
		{request.Dependencies, "-deps"},
		{request.Tests, "-test"},
		{request.Compiled, "-compiled"},
		{request.Export, "-export"},
		{request.WithoutVersionControl, "-buildvcs=false"},
	} {
		if flag.asked {
			arguments = append(arguments, flag.flag)
		}
	}
	for _, field := range request.Fields {
		if field == "" || strings.Contains(field, ",") {
			return nil, fmt.Errorf("%q isn't one field go list's -json can name", field)
		}
	}
	if len(request.Fields) == 0 {
		arguments = append(arguments, "-json")
	} else {
		arguments = append(arguments, "-json="+strings.Join(request.Fields, ","))
	}
	arguments = append(arguments, request.BuildFlags...)
	for _, pattern := range request.Patterns {
		if strings.HasPrefix(pattern, "-") {
			return nil, fmt.Errorf("the pattern %q would be read as a flag", pattern)
		}
	}
	return append(arguments, request.Patterns...), nil
}

// Run runs command, a go list the caller built (its go, directory and environment the caller's own),
// and returns the packages its output names, Decoded. It reads the command's standard output, so a
// command whose Stdout is set already is refused unrun, as exec refuses it. A run that fails, or exits
// nonzero, is an error naming the command and carrying its standard error, unless the caller sent
// that elsewhere by setting Stderr.
func Run(command *exec.Cmd) ([]Package, error) {
	output, outputError := command.Output()
	if outputError != nil {
		standardError := ""
		var exitError *exec.ExitError
		if errors.As(outputError, &exitError) {
			standardError = strings.TrimSpace(string(exitError.Stderr))
		}
		return nil, fmt.Errorf("%s: %w\n%s", strings.Join(command.Args, " "), outputError, standardError)
	}
	packages, decodeError := Decode(output)
	if decodeError != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(command.Args, " "), decodeError)
	}
	return packages, nil
}
