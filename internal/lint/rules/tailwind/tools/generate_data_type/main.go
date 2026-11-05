// Command generate_data_type captures what the real Tailwind engine infers for a corpus of CSS
// value shapes, so that the Go port in internal/tailwind can be tested against measurements rather
// than against a reading of the TypeScript source.
//
// Usage:
//
//	go run ./internal/lint/rules/tailwind/tools/generate_data_type -package-root <tailwindcss package> [-output <file.json>] [-check]
//
// The distinction this tool exists to enforce is that reading the source and asking the engine are
// different claims, and they disagree. Read `infer-data-type.ts` and `serif` is a generic-name.
// Ask the engine with the full type list and `serif` is a family-name, because family-name is
// earlier in the list and also matches. Every expectation in the emitted fixture is an answer the
// shipped engine gave, so a port that agrees with it agrees with production.
//
// `-check` regenerates and fails when the committed fixture disagrees, which is what makes a
// Tailwind upgrade that changes an answer show up as a failing gate instead of a silent behavior
// change. Like generate_collapse, nothing invokes it automatically yet; it is a manual gate.
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
		filepath.Join("internal", "tailwind", "testdata", "datatype_fixtures.json"),
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
			fmt.Fprintf(os.Stderr, "read %s: %v\n", *output, readError)
			os.Exit(1)
		}
		// Go whitespace: this tool comparing its own committed fixture with its own output.
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(
				os.Stderr,
				"%s is stale: the engine now answers differently. Re-run without -check and read the diff.\n",
				*output,
			)
			os.Exit(1)
		}
		// Ignored: the check has passed, and the exit code says so whether or not this lands.
		fmt.Fprintf(os.Stderr, "%s matches the engine\n", *output)
		return
	}

	if writeError := os.WriteFile(*output, generated, 0o644); writeError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "write %s: %v\n", *output, writeError)
		os.Exit(1)
	}
	// Ignored: what it reports is already written, and a summary that cannot reach the terminal changes none of it.
	fmt.Fprintf(os.Stderr, "wrote %s\n", *output)
}

// enumerate runs the Node side and returns its JSON, verified to be well-formed and non-trivial.
//
// The size check is not decoration. A corpus that silently shrank to a handful of cases is the
// failure mode this whole approach exists to prevent, and it would otherwise look like a passing
// run.
func enumerate(packageRoot string) ([]byte, error) {
	directory, err := toolDirectory()
	if err != nil {
		return nil, err
	}
	script := filepath.Join(directory, "enumerate.mjs")

	command := exec.Command("node", script, packageRoot)
	command.Stderr = os.Stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", script, err)
	}

	var parsed struct {
		TailwindVersion string `json:"tailwindVersion"`
		Cases           []any  `json:"cases"`
	}
	if decodeError := json.Unmarshal(out, &parsed); decodeError != nil {
		return nil, fmt.Errorf("parse enumeration: %w", decodeError)
	}
	if parsed.TailwindVersion == "" {
		return nil, fmt.Errorf("enumeration reported no Tailwind version")
	}
	if len(parsed.Cases) < 400 {
		return nil, fmt.Errorf(
			"enumeration produced only %d cases; the corpus is meant to be several hundred shapes",
			len(parsed.Cases),
		)
	}
	return out, nil
}
