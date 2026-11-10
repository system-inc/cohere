package main

import (
	"fmt"
	"io"
	"os"
)

// runGoStdin is the editor's save in a Go module or workspace, `cohere --fix [--format] --stdin-filepath <path>` with
// the buffer on input: what `--fix` would write for that file once the rest of the workspace type-checks, computed by
// the Go engine from the buffer and written to output, with nothing written to disk (go_engine's pipeline.runStdin).
// The outcomes are runStdin's for TypeScript, for the save hook to act on: the file's text, or the buffer unchanged for
// a file the engine declines, on output with exit 0; or nothing on output and a non-zero exit, with why on stderr, for
// an engine or formatter that broke.
//
// The engine's output is its text rather than records, so it is read whole and passed through, never rendered. And it
// is held to one promise before it is: exit 0 with no text for a buffer that had some would empty the file, so it is a
// failure, never a save.
func runGoStdin(
	location projectLocation,
	given map[string]bool,
	positionals []string,
	input io.Reader,
	output io.Writer,
) (int, error) {
	// As main refuses a TypeScript save: --fix is required rather than implied, so the command line says what the
	// output means, and no other phase or path rides along.
	switchOn := func(name string) bool { return given[name] && flagValue(name) == "true" }
	if !switchOn("fix") || switchOn("no-fix") || switchOn("types") || switchOn("lint") || len(positionals) > 0 {
		return 1, errorStdinNeedsFix
	}
	language := goLanguage(goContainer(location.Root))
	arguments, _, argumentsError := engineArguments(language, goFlags, location, given, flagValue, positionals)
	if argumentsError != nil {
		return 1, argumentsError
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		return 1, fmt.Errorf("finding this cohere's own executable, which runs as the Go engine: %w", executableError)
	}
	buffer, readError := io.ReadAll(input)
	if readError != nil {
		return 1, fmt.Errorf("reading the buffer from stdin: %w", readError)
	}
	text, exitCode, ended, runError := runEngineForText(executable, append([]string{goEngineVerb}, arguments...),
		[]string{goEngineChildVariable + "=1"}, buffer, os.Stderr)
	if runError != nil {
		return 1, runError
	}
	if exitCode < 0 {
		return 1, fmt.Errorf("the Go engine %s, so %s was not formatted", ended, flagValue("stdin-filepath"))
	}
	if exitCode != 0 {
		// The engine said why on stderr, and its output is no answer to pass on.
		return exitCode, nil
	}
	if len(text) == 0 && len(buffer) > 0 {
		return 1, fmt.Errorf("the Go engine answered the save of %s with no text, which would empty the file, so "+
			"nothing was written", flagValue("stdin-filepath"))
	}
	_, writeError := output.Write(text)
	return 0, writeError
}
