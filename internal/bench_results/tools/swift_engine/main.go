// Command swift_engine prints the path of the Swift engine the launcher runs beside a cohere engine built
// from a commit, building it first when the launcher's cache has none, so the quiet bench can pin both
// engines of one build (#ywx8zgd):
//
//	swift_engine <cohere checkout> <commit> <contract>
//
// The engine is the one dispatch.ResolveSwiftEngine names for that checkout, commit, Swift contract and the
// installed toolchain: the same file a cohere engine built from that commit resolves on its own, since that is
// how it resolves it. The checkout is given rather than found, because the bench runs from inside a copy of a
// project that may itself be a cohere checkout, which would be found first. The commit is the full one: the
// launcher keys the engine on it.
//
// It exits 1 when no engine could be resolved, so the bench stops rather than measuring a build it cannot name.
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

func main() {
	if len(os.Args) != 4 {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintln(os.Stderr, "usage: swift_engine <cohere checkout> <commit> <contract>")
		os.Exit(2)
	}
	contract, contractError := strconv.Atoi(os.Args[3])
	if contractError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "swift_engine: the contract %q is not a number\n", os.Args[3])
		os.Exit(2)
	}
	versionOutput, swiftVersionOutputError := dispatch.SwiftVersionOutput()
	if swiftVersionOutputError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "swift_engine: %v\n", swiftVersionOutputError)
		os.Exit(1)
	}
	binaryPath, _, resolveSwiftEngineError := dispatch.ResolveSwiftEngine(
		dispatch.DefaultPaths(os.Args[1]),
		os.Args[2],
		contract,
		versionOutput,
	)
	if resolveSwiftEngineError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "swift_engine: %v\n", resolveSwiftEngineError)
		os.Exit(1)
	}
	if _, printError := fmt.Println(binaryPath); printError != nil {
		// Ignored: the exit code below says the path never arrived even when standard error cannot.
		fmt.Fprintf(os.Stderr, "swift_engine: printing the path: %v\n", printError)
		os.Exit(1)
	}
}
