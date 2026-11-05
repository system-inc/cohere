package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/lint/registry"
)

// The parallel format pass may only make the fix phase faster. So the phase runs twice over the same tree,
// once with every file on the serial path and once with the parallel pass, and the two must agree on
// everything: the summary, the would-change list, and in a writing run the bytes left on disk.
//
// The tree holds each kind of file the pass sorts: formatted TypeScript (the majority, done in parallel),
// unformatted TypeScript (its formatting leads back to the rules, so it returns to the serial path), files
// with proposals (serial from the start), and markdown, CSS and JSON, which have no rules to ask. Run under
// -race, it is also the race test: the printers run side by side here as they do on a first run.
// Not parallel: it sets the package-level parallelFormatWorkers, which the fix phase's format pass reads
func TestTheParallelFormatPassMatchesTheSerialOne(test *testing.T) {
	files := map[string]string{
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": { \"no-debugger\": \"error\" } }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		"Ugly.ts":                  "export const ugly   =   1\n",
		"Fixable.ts":               "export function fixable(): number {\n  debugger;\n  return 1;\n}\n",
		"FixableUgly.ts":           "export function both(): number {\n    debugger;\n    return 2\n}\n",
		"notes.md":                 "#  Notes\n\nSome   text.\n",
		"tidy.md":                  "# Tidy\n\nText.\n",
		"style.css":                ".a{color:red}\n",
		"data.json":                "{\"a\":1}\n",
	}
	for index := range 60 {
		files[fmt.Sprintf("source/Tidy%02d.ts", index)] = fmt.Sprintf("export const tidy%d = %d;\n", index, index)
	}

	run := func(test *testing.T, workers int, write bool) (string, map[string]string) {
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

		summary, _, _, applyProposedFixesError := applyProposedFixes(
			context.Background(),
			graph,
			graph.ProjectFiles(),
			registry.All(),
			func() (edit.Transform, []string) { return formatTransform(engine), enumeration.Files },
			wholeTreeScope(),
			graph.Anchor.Root(),
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
		// Absolute paths differ between the two trees, so the summary is compared with the root taken out.
		described = stripRoot(described, directory)

		onDisk := map[string]string{}
		for name := range files {
			contents, readError := os.ReadFile(filepath.Join(directory, name))
			if readError != nil {
				test.Fatal(readError)
			}
			onDisk[name] = string(contents)
		}
		return described, onDisk
	}

	for _, write := range []bool{false, true} {
		serial, serialDisk := run(test, 0, write)
		parallel, parallelDisk := run(test, 8, write)
		if serial != parallel {
			test.Errorf(
				"write=%v: the parallel pass reported differently:\n--- serial\n%s\n--- parallel\n%s",
				write,
				serial,
				parallel,
			)
		}
		if !reflect.DeepEqual(serialDisk, parallelDisk) {
			for name := range serialDisk {
				if serialDisk[name] != parallelDisk[name] {
					test.Errorf(
						"write=%v: %s differs on disk:\n--- serial\n%s--- parallel\n%s",
						write,
						name,
						serialDisk[name],
						parallelDisk[name],
					)
				}
			}
		}
		// The control that the comparison could fail: the tree must have had something to change.
		if write && serialDisk["Ugly.ts"] == files["Ugly.ts"] {
			test.Fatalf("the writing run changed nothing, so the comparison proves nothing:\n%s", serial)
		}
	}
}

func stripRoot(text string, root string) string {
	if resolved, resolveSymlinksError := filepath.EvalSymlinks(root); resolveSymlinksError == nil {
		text = strings.ReplaceAll(text, resolved, "ROOT")
	}
	return strings.ReplaceAll(text, root, "ROOT")
}

// The pass's own sorting: a file with proposals is never attempted, a file whose attempt asked for its
// rules is left to the serial path, a file that failed keeps its failure for the serial loop to report in
// order, and every other file is done.
func TestFormatInParallelLeavesTheRulesToTheSerialPath(test *testing.T) {
	test.Parallel()
	fileNames := []string{"/a.ts", "/b.ts", "/c.ts", "/d.md"}
	byFileName := map[string][]edit.Proposal{"/a.ts": {{RuleName: "no-debugger"}}}
	failure := errors.New("does not parse")
	attempted := map[string]bool{}
	var mutex sync.Mutex

	attempts := formatInParallel(fileNames, byFileName, nil, func(fileName string) (edit.FileResult, error) {
		mutex.Lock()
		attempted[fileName] = true
		mutex.Unlock()
		switch fileName {
		case "/b.ts":
			return edit.FileResult{}, fmt.Errorf(
				"collecting fixes for %s after formatting: %w",
				fileName,
				errorRelintRefused,
			)
		case "/c.ts":
			return edit.FileResult{FileName: fileName}, failure
		}
		return edit.FileResult{FileName: fileName, Converged: true}, nil
	})

	if attempted["/a.ts"] {
		test.Error("a file with proposals was attempted off the serial path")
	}
	if attempts[0].done || attempts[1].done {
		test.Errorf(
			"a file with proposals or one that asked for its rules was marked done: %+v %+v",
			attempts[0],
			attempts[1],
		)
	}
	if !attempts[2].done || !errors.Is(attempts[2].err, failure) {
		test.Errorf("a failure was not kept for the serial loop to report: %+v", attempts[2])
	}
	if !attempts[3].done || attempts[3].result.FileName != "/d.md" || attempts[3].err != nil {
		test.Errorf("a file with nothing to ask was not done: %+v", attempts[3])
	}
}

// A speculative format result stands only where the parallel pass after the walk would have computed the
// same one (#679s763): for a file the walk proposed nothing for, and for the bytes the walk read. A result
// computed from other bytes than the program holds, a file edited between the two reads, is discarded, and so
// is one for a file with a proposal; the file then takes the path it always took.
func TestASpeculativeFormatResultStandsOnlyForTheWalksBytes(test *testing.T) {
	test.Parallel()
	graph, directory := buildNarrowFixtureGraph(test, map[string]string{"A.ts": "export const a = 1;\n"})
	fileName := filepath.Join(directory, "A.ts")
	speculation := func(read string) *formatSpeculation {
		done := make(chan struct{})
		close(done)
		return &formatSpeculation{
			done: done,
			attempts: map[string]formatAttempt{
				fileName: {result: edit.FileResult{FileName: fileName, Text: read}, done: true},
			},
			read: map[string]string{fileName: read},
		}
	}

	if _, kept := speculation("export const a = 1;\n").keepable(nil, graph)[fileName]; !kept {
		test.Fatal("a result computed from the walk's own bytes was discarded, so nothing below is about the bytes")
	}
	if _, kept := speculation("export const a = 2;\n").keepable(nil, graph)[fileName]; kept {
		test.Error("a result computed from other bytes than the walk read was kept")
	}
	proposals := map[string][]edit.Proposal{fileName: {{RuleName: "no-debugger"}}}
	if _, kept := speculation("export const a = 1;\n").keepable(proposals, graph)[fileName]; kept {
		test.Error("a result for a file the walk proposed a fix for was kept")
	}
}
