package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/lint/registry"
)

// A run that writes nothing keeps the speculation's changed result for a file with no rules, so a markdown or
// json file the printer changes is formatted once rather than twice: the pass after the walk would only have
// checked it again, to the same result. Counted at the transform, file by file, and the run's report is held
// to the serial run's, the one with no speculation at all.
//
// The clean cases are the ones the keep must not reach: a TypeScript file the printer changes is still
// discarded and formatted again, since its rules must be asked about the printed text, and a writing run still
// formats a changed markdown file after the walk and writes it.
// Not parallel: it sets the package-level parallelFormatWorkers, which the fix phase's format pass reads
func TestANoFixRunFormatsAChangedFileWithNoRulesOnce(test *testing.T) {
	files := map[string]string{
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": { \"no-debugger\": \"error\" } }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		"Ugly.ts":                  "export const ugly   =   1\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
		"notes.md":                 "#  Notes\n\nSome   text.\n",
		"tidy.md":                  "# Tidy\n\nText.\n",
		"data.json":                "{\"a\":1}\n",
	}

	type outcome struct {
		described string
		formats   map[string]int
		onDisk    map[string]string
	}
	run := func(test *testing.T, workers int, write bool) outcome {
		test.Helper()
		graph, directory := buildNarrowFixtureGraph(test, files)
		location := projectLocation{
			Root:               directory,
			ConfigFileName:     filepath.Join(directory, "tsconfig.json"),
			LintConfigFileName: filepath.Join(directory, "CohereSettings.json"),
		}
		if _, configureLintError := configureLint(graph, location, nil); configureLintError != nil {
			test.Fatal(configureLintError)
		}
		engine, newResolvingError := native.NewResolving(directory)
		if newResolvingError != nil {
			test.Fatal(newResolvingError)
		}
		enumeration, enumerateError := engine.Enumerate(directory)
		if enumerateError != nil {
			test.Fatal(enumerateError)
		}

		previous := parallelFormatWorkers
		parallelFormatWorkers = func() int { return workers }
		defer func() { parallelFormatWorkers = previous }()

		// Every format of every file, the speculation's and the pass after the walk's alike.
		formats := map[string]int{}
		var mutex sync.Mutex
		inner := formatTransform(engine)
		counting := func(fileName string, text string, parsed *ast.SourceFile) (string, error) {
			mutex.Lock()
			formats[filepath.Base(fileName)]++
			mutex.Unlock()
			return inner(fileName, text, parsed)
		}

		summary, _, _, applyProposedFixesError := applyProposedFixes(
			context.Background(),
			graph,
			graph.ProjectFiles(),
			registry.All(),
			func() (edit.Transform, []string) { return counting, enumeration.Files },
			wholeTreeScope(),
			graph.Anchor.Root(),
			writeRootOf(test, graph.Anchor.Root()),
			0,
			write,
			nil,
		)
		if applyProposedFixesError != nil {
			test.Fatal(applyProposedFixesError)
		}
		described := summary.String()
		for _, changed := range summary.ChangedFiles {
			described += fmt.Sprintf("\n%s: %v", relativeIn(test, directory, changed.FileName), changed.Changers)
		}

		onDisk := map[string]string{}
		for name := range files {
			contents, readError := os.ReadFile(filepath.Join(directory, name))
			if readError != nil {
				test.Fatal(readError)
			}
			onDisk[name] = string(contents)
		}
		return outcome{described: stripRoot(described, directory), formats: formats, onDisk: onDisk}
	}

	checked := run(test, 8, false)
	for _, name := range []string{"notes.md", "data.json", "tidy.md"} {
		if checked.formats[name] != 1 {
			test.Errorf("under --no-fix %s was formatted %d times, want once: %v", name, checked.formats[name],
				checked.formats)
		}
	}
	if checked.formats["Ugly.ts"] < 2 {
		test.Errorf("under --no-fix the changed TypeScript file was formatted %d times, so its printed text was "+
			"never re-linted after the walk: %v", checked.formats["Ugly.ts"], checked.formats)
	}

	// The report is the serial run's: the same summary and the same would-change files, which is the verdict.
	serial := run(test, 0, false)
	if checked.described != serial.described {
		test.Errorf("the kept results reported otherwise:\n--- serial\n%s\n--- speculated\n%s",
			serial.described, checked.described)
	}
	// The control that the comparison could fail: the serial run reports each changed file as one --fix rewrites.
	for _, name := range []string{"notes.md", "data.json", "Ugly.ts"} {
		if !strings.Contains(serial.described, "\n"+name+": [format]") {
			test.Fatalf("the serial run did not report %s as changed, so nothing above is about it:\n%s", name,
				serial.described)
		}
	}
	if checked.onDisk["notes.md"] != files["notes.md"] {
		test.Errorf("--no-fix wrote notes.md:\n%s", checked.onDisk["notes.md"])
	}

	written := run(test, 8, true)
	if written.formats["notes.md"] < 2 {
		test.Errorf("under --fix the changed markdown file was formatted %d times, so the speculation's check "+
			"was kept in place of the write: %v", written.formats["notes.md"], written.formats)
	}
	if written.onDisk["notes.md"] == files["notes.md"] {
		test.Errorf("under --fix the changed markdown file was not written:\n%s", written.onDisk["notes.md"])
	}
}
