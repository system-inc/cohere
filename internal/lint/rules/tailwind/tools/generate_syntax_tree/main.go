// Command generate_syntax_tree captures compiled utility trees from the real Tailwind engine, together
// with the {order, count} reading the engine takes from each, so that the Go AST and PropertySort
// in internal/tailwind are tested against measurements rather than against a reading of the
// TypeScript source.
//
// Usage:
//
//	go run ./internal/lint/rules/tailwind/tools/generate_syntax_tree \
//	    -package-root <tailwindcss package> [-output <file.json>] [-check]
//
// The distinction this tool exists to enforce is the same one generate_data_type enforces, and
// it bites harder here. `getPropertySort` looks like a tree walk and is not one: it is a
// breadth-first queue that descends only into `rule` and `at-rule`. A port that reads the source
// quickly and reaches for the file's own `walk` helper gets a depth-first traversal that also
// descends into `at-root`, and that port agrees with the engine on the overwhelming majority of
// real input. The fixture carries the trees where it does not.
//
// `-check` regenerates and fails when the committed fixture disagrees, which is what makes a
// Tailwind upgrade that changes a tree shape or a reading show up as a failing gate instead of a
// silent behavior change. Like generate_data_type and generate_collapse, nothing invokes it
// automatically; it is a manual gate.
//
// Node is required to run it and never to use the result.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	packageRoot := flag.String(
		"package-root",
		"",
		"path to the tailwindcss package root (the directory holding package.json)",
	)
	output := flag.String(
		"output",
		filepath.Join("internal", "tailwind", "testdata", "ast_fixtures.json"),
		"where to write the fixture",
	)
	check := flag.Bool("check", false, "regenerate and fail if the committed fixture disagrees")
	flag.Parse()

	if *packageRoot == "" {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintln(os.Stderr, "-package-root is required")
		os.Exit(2)
	}

	generated, enumerateError := enumerate(*packageRoot)
	if enumerateError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "enumerate: %v\n", enumerateError)
		os.Exit(1)
	}

	if *check {
		committed, readError := os.ReadFile(*output)
		if readError != nil {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(os.Stderr, "read committed fixture: %v\n", readError)
			os.Exit(1)
		}
		// Go whitespace: this tool comparing its own committed fixture with its own output.
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(os.Stderr, "%s is out of date; rerun without -check\n", *output)
			os.Exit(1)
		}
		// Ignored: the check has passed, and the exit code says so whether or not this lands.
		fmt.Fprintf(os.Stderr, "%s is up to date\n", *output)
		return
	}

	if writeError := os.WriteFile(*output, generated, 0o644); writeError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *output, writeError)
		os.Exit(1)
	}

	var summary struct {
		TailwindVersion    string `json:"tailwindVersion"`
		ClassListSize      int    `json:"classListSize"`
		DistinctSignatures int    `json:"distinctSignatures"`
		FullList           struct {
			Compiled                          int `json:"compiled"`
			BreadthFirstDiffersFromDepthFirst int `json:"breadthFirstDiffersFromDepthFirst"`
			WithAtRootSubtree                 int `json:"withAtRootSubtree"`
		} `json:"fullList"`
		Cases          []json.RawMessage `json:"cases"`
		TraversalCases []json.RawMessage `json:"traversalCases"`
	}
	if decodeError := json.Unmarshal(generated, &summary); decodeError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "summarize: %v\n", decodeError)
		os.Exit(1)
	}

	// Ignored: what it reports is already written, and a summary that cannot reach the terminal changes none of it.
	fmt.Fprintf(
		os.Stderr,
		"wrote %s: tailwind %s, %d cases over %d distinct tree shapes, %d traversal-divergence cases; "+
			"of %d compiled candidates in the full class list, %d carry an at-root subtree and %d visit differently breadth-first than depth-first\n",
		*output,
		summary.TailwindVersion,
		len(summary.Cases),
		summary.DistinctSignatures,
		len(summary.TraversalCases),
		summary.FullList.Compiled,
		summary.FullList.WithAtRootSubtree,
		summary.FullList.BreadthFirstDiffersFromDepthFirst,
	)
}

// enumerate runs the Node side and returns its JSON.
//
// The engine is JavaScript and there is no faithful way to ask it these questions from Go. The
// boundary is here, at generation time, and never at lint time: the committed fixture is what the
// test reads.
func enumerate(packageRoot string) ([]byte, error) {
	directory, toolDirectoryError := toolDirectory()
	if toolDirectoryError != nil {
		return nil, toolDirectoryError
	}
	script := filepath.Join(directory, "enumerate.mjs")

	command := exec.Command("node", script, packageRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if runError := command.Run(); runError != nil {
		return nil, fmt.Errorf("node %s: %w\n%s", script, runError, stderr.String())
	}
	return stdout.Bytes(), nil
}
