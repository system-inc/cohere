// Command generate_class_literals asks better-tailwindcss itself what it reads as a class string
// (#btxd64n), and writes two files from its answers:
//
//   - default_selectors.json, upstream's DEFAULT_SELECTORS, which the tailwind package embeds as its
//     defaults, so they are upstream's by construction rather than transcribed;
//   - testdata/class_literals/upstream.json, the literals upstream's own listener hands a rule for
//     each case in testdata/class_literals/cases.json, which TestClassLiteralsAgreeWithUpstream
//     compares with the Go reader range for range.
//
// Usage:
//
//	go run ./internal/lint/rules/tailwind/tools/generate_class_literals [-packages <directory or corpus spelling>] [-check]
//
// Run from the repository root. -packages names where eslint, the plugin and typescript-eslint's
// parser resolve, the ahra corpus by default.
//
// # -check
//
// Regenerates and fails when either committed file disagrees, so a plugin upgrade that changes its
// defaults or what its matchers read shows up as a failing gate and a diff rather than as a finding
// that quietly stops appearing. Like every other generate_*, nothing invokes it automatically.
//
// Node is required to produce the files and never to use them.
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
	packages := flag.String(
		"packages",
		"ahra:.",
		"a directory, or a corpus spelling, where eslint, eslint-plugin-better-tailwindcss and @typescript-eslint/parser resolve",
	)
	check := flag.Bool("check", false, "regenerate and fail if a committed file disagrees")
	flag.Parse()

	toolDirectory, err := toolDirectory()
	if err != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "locating this tool: %v\n", err)
		os.Exit(1)
	}
	packageDirectory := filepath.Join(toolDirectory, "..", "..")
	casesPath := filepath.Join(packageDirectory, "testdata", "class_literals", "cases.json")

	command := exec.Command("node", filepath.Join(toolDirectory, "extract.mjs"), *packages, casesPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if runError := command.Run(); runError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "node extract.mjs: %v\n%s", runError, stderr.String())
		os.Exit(1)
	}

	var extracted struct {
		Plugin           string          `json:"plugin"`
		DefaultSelectors json.RawMessage `json:"defaultSelectors"`
		Cases            json.RawMessage `json:"cases"`
	}
	if decodeError := json.Unmarshal(stdout.Bytes(), &extracted); decodeError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "reading extract.mjs's output: %v\n", decodeError)
		os.Exit(1)
	}

	defaults, err := indent(extracted.DefaultSelectors)
	if err != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "default selectors: %v\n", err)
		os.Exit(1)
	}
	// The oracle leaves the defaults out: they are default_selectors.json's to hold, and a copy here
	// would be a second place for them to drift.
	withoutDefaults, err := json.Marshal(struct {
		Plugin string          `json:"plugin"`
		Cases  json.RawMessage `json:"cases"`
	}{extracted.Plugin, extracted.Cases})
	if err != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "oracle: %v\n", err)
		os.Exit(1)
	}
	oracle, err := indent(withoutDefaults)
	if err != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "oracle: %v\n", err)
		os.Exit(1)
	}
	outputs := []struct {
		path     string
		contents []byte
	}{
		{filepath.Join(packageDirectory, "default_selectors.json"), defaults},
		{filepath.Join(packageDirectory, "testdata", "class_literals", "upstream.json"), oracle},
	}

	stale := false
	for _, output := range outputs {
		if *check {
			committed, readError := os.ReadFile(output.path)
			if readError != nil {
				// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this
				// lands.
				fmt.Fprintf(os.Stderr, "read %s: %v\n", output.path, readError)
				os.Exit(1)
			}
			if !bytes.Equal(committed, output.contents) {
				// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this
				// lands.
				fmt.Fprintf(
					os.Stderr,
					"%s is stale: better-tailwindcss %s answers differently. Re-run without -check and read the diff.\n",
					output.path,
					extracted.Plugin,
				)
				stale = true
			}
			continue
		}
		if writeError := os.WriteFile(output.path, output.contents, 0o644); writeError != nil {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(os.Stderr, "write %s: %v\n", output.path, writeError)
			os.Exit(1)
		}
	}
	if stale {
		os.Exit(1)
	}
	if *check {
		// Ignored: the check has passed, and the exit code says so whether or not this lands.
		fmt.Fprintf(os.Stderr, "both files match better-tailwindcss %s\n", extracted.Plugin)
		return
	}
	// Ignored: what it reports is already written, and a summary that cannot reach the terminal changes none of it.
	fmt.Fprintf(os.Stderr, "wrote the default selectors and the oracle from better-tailwindcss %s\n", extracted.Plugin)
}

// indent writes JSON with four spaces and a final newline, so a regenerated file diffs line by line.
func indent(raw []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if indentError := json.Indent(&buffer, raw, "", "    "); indentError != nil {
		return nil, indentError
	}
	buffer.WriteByte('\n')
	return buffer.Bytes(), nil
}
