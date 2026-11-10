package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// A stopped speculation finishes the files its workers are on and begins no more, so every file it did not
// reach is left to formatInParallel, which computes it as it always did, and the run's output is the same
// (#679s763). Never stopped, it keeps every file it leaves unchanged: the control, without which a
// speculation that attempted nothing would pass.
func TestAStoppedSpeculationBeginsNoMoreFiles(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	var candidates []string
	for index := range 40 {
		fileName := filepath.Join(directory, fmt.Sprintf("f%02d.md", index))
		if writeError := os.WriteFile(fileName, []byte("# Title\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		candidates = append(candidates, fileName)
	}
	workers := speculationWorkers()
	if workers == 0 || workers >= len(candidates) {
		test.Skipf(
			"%d speculation workers for %d files, so a stop could not leave any file unbegun",
			workers,
			len(candidates),
		)
	}

	// A formatter that rewrites every third file, so the pass after the walk has real work as well as kept
	// results to take.
	format := func(fileName string, text string, _ *ast.SourceFile) (string, error) {
		if index := slices.Index(candidates, fileName); index%3 == 0 {
			return strings.ToUpper(text), nil
		}
		return text, nil
	}
	// Each transform reports that it began, then holds its file until the test lets it go, so the stop lands
	// while every worker is mid-file.
	started := make(chan struct{}, len(candidates))
	release := make(chan struct{})
	holding := func(fileName string, text string, parsed *ast.SourceFile) (string, error) {
		started <- struct{}{}
		<-release
		return format(fileName, text, parsed)
	}

	stopped := speculateFormat(candidates, holding, 1, true, nil)
	for range workers {
		<-started
	}
	stopped.stop()
	close(release)
	stopped.wait()
	if len(stopped.attempts) > workers {
		test.Errorf("stopped with %d workers mid-file, the speculation kept %d files", workers, len(stopped.attempts))
	}

	// What the run reports is the same whether speculation was stopped or never ran: the pass after the walk
	// computes every file the stopped speculation did not keep.
	process := func(fileName string) (edit.FileResult, error) {
		return edit.CheckFile(fileName, func(string, string) ([]edit.Proposal, error) { return nil, nil }, format, 1)
	}
	withStop := formatInParallel(candidates, nil, stopped.keepable(nil, nil), process)
	without := formatInParallel(candidates, nil, nil, process)
	for index, fileName := range candidates {
		if withStop[index].done != without[index].done || withStop[index].result.Text != without[index].result.Text ||
			withStop[index].result.Changed != without[index].result.Changed {
			test.Errorf(
				"%s: after a stopped speculation %+v, without one %+v",
				filepath.Base(fileName),
				withStop[index],
				without[index],
			)
		}
	}

	free := speculateFormat(candidates, format, 1, true, nil)
	free.wait()
	if want := len(candidates) - (len(candidates)+2)/3; len(free.attempts) != want {
		test.Fatalf(
			"never stopped, the speculation kept %d of %d files, want the %d it leaves unchanged",
			len(free.attempts),
			len(candidates),
			want,
		)
	}
}

// A run with no program, `--format-only`'s, has no walk for the speculation to overlap and no type check beside
// it, so the fix phase stops the speculation once the plan is taken and formatInParallel formats the rest on
// every core (#9h6g0s3). Left running, it formatted a copy of adamic on a quarter of the cores while the rest
// sat idle. A run with a walk and no fused check keeps its speculation to the end, since its type check is still
// running: the control, and the half a stop on every run would break.
//
// Seen as how many files are formatted at once: the speculation never has more than speculationWorkers on
// them, and formatInParallel, given four workers more, does only once the speculation is stopped.
// Not parallel: it sets the package-level parallelFormatWorkers, which the fix phase's format pass reads
func TestARunWithNoWalkStopsItsSpeculation(test *testing.T) {
	files := map[string]string{
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": { \"no-debugger\": \"error\" } }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
	}
	for index := range 64 {
		files[fmt.Sprintf("notes/n%02d.md", index)] = "# Title\n"
	}
	speculating := speculationWorkers()
	previous := parallelFormatWorkers
	parallelFormatWorkers = func() int { return speculating + 4 }
	defer func() { parallelFormatWorkers = previous }()

	// The most files formatted at once over one run. Each format takes long enough that the speculation is
	// still going when the fix phase decides whether to stop it.
	run := func(test *testing.T, graph *program.Graph, directory string) int {
		test.Helper()
		var candidates []string
		for name := range files {
			if strings.HasSuffix(name, ".md") {
				candidates = append(candidates, filepath.Join(directory, name))
			}
		}
		var mutex sync.Mutex
		active, peak := 0, 0
		holding := func(_ string, text string, _ *ast.SourceFile) (string, error) {
			mutex.Lock()
			active++
			peak = max(peak, active)
			mutex.Unlock()
			time.Sleep(10 * time.Millisecond)
			mutex.Lock()
			active--
			mutex.Unlock()
			return text, nil
		}
		var projectFiles []*ast.SourceFile
		var rules []rule.Entry
		if graph != nil {
			projectFiles, rules = graph.ProjectFiles(), registry.All()
		}
		_, _, _, applyProposedFixesError := applyProposedFixes(
			context.Background(),
			graph,
			projectFiles,
			rules,
			func() (edit.Transform, []string) { return holding, candidates },
			wholeTreeScope(),
			directory,
			writeRootOf(test, directory),
			1,
			false,
			nil,
		)
		if applyProposedFixesError != nil {
			test.Fatal(applyProposedFixesError)
		}
		return peak
	}

	noProgram := test.TempDir()
	for name, contents := range files {
		path := filepath.Join(noProgram, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if peak := run(test, nil, noProgram); peak <= speculating {
		test.Errorf("with no walk, at most %d files were formatted at once, the speculation's %d: it was never "+
			"stopped, so formatInParallel's %d workers had nothing left", peak, speculating, speculating+4)
	}

	graph, directory := buildNarrowFixtureGraph(test, files)
	location := projectLocation{
		Root:               directory,
		ConfigFileName:     filepath.Join(directory, "tsconfig.json"),
		LintConfigFileName: filepath.Join(directory, "CohereSettings.json"),
	}
	if _, configureLintError := configureLint(graph, location, nil); configureLintError != nil {
		test.Fatal(configureLintError)
	}
	if len(graph.ProjectFiles()) == 0 {
		test.Fatal("the fixture graph holds no project files, so its run has no walk")
	}
	if peak := run(test, graph, directory); peak > speculating {
		test.Errorf("with a walk and no fused check, %d files were formatted at once, more than the speculation's "+
			"%d: it was stopped while the type check beside it still wanted the cores", peak, speculating)
	}
}
