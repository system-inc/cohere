//go:build unix

// Beside go_engine_test.go, whose helpers for a Go run's command line it reads, so the file builds on Unix as that one
// does.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/lint/configuration"
)

// goTimelineStream is the Go contract's timeline fixture: a clean run whose timeline record, four stamps, comes last
// before its summary.
func goTimelineStream(test *testing.T) []string {
	test.Helper()
	return contractFixture(test, "GoTimeline.jsonl")
}

// goTimelineIndex is where the timeline record is in lines, failing the test when it is nowhere.
func goTimelineIndex(test *testing.T, lines []string) int {
	test.Helper()
	index := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, `{"kind":"timeline"`) })
	if index < 0 {
		test.Fatal("GoTimeline.jsonl holds no timeline record")
	}
	return index
}

// renderGoTimeline renders a Go stream under one output mode, through a run that asked for `--timeline` or did not,
// and gives the output with the refusal of the record that broke the stream, or the finish's error.
func renderGoTimeline(test *testing.T, mode outputMode, timelineRequested bool, lines []string) (string, error) {
	test.Helper()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, goLanguage("module"), "", time.Now())
	run.timelineRequested = timelineRequested
	for _, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			flushStream(test, stream)
			return out.String(), acceptError
		}
	}
	_, finishError := run.finish(0, "exited 0")
	flushStream(test, stream)
	return out.String(), finishError
}

// TestTimelineReachesTheGoEngineAlone: `--timeline` is taken, not refused, and forwarded to the Go engine, which
// answers it with its timeline record (#3v2zp7g). Swift's run refuses it by name, discovery hands it to no project's
// run but a Go one's, and a run at no Go project refuses it, as `--host-build-only` is refused, saying what it asks of
// a Go run.
func TestTimelineReachesTheGoEngineAlone(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	given := map[string]bool{"no-fix": true, "timeline": true}
	arguments, mode, argumentsError := engineArguments(goLanguage("module"), goFlags, goTestLocation(root), given,
		typedTrue, nil)
	if argumentsError != nil {
		test.Fatalf("--timeline was refused for Go: %v", argumentsError)
	}
	expected := []string{"--contract", "3", "--root", root, "--no-fix", "--timeline"}
	if !slices.Equal(arguments, expected) || mode != engineModeCheck {
		test.Errorf("arguments are\n  %q in mode %s\nexpected\n  %q in mode %s", arguments, mode, expected,
			engineModeCheck)
	}

	_, _, swiftError := engineArguments(swiftLanguage, swiftFlags, goTestLocation(root), given, typedTrue, nil)
	if swiftError == nil || !strings.Contains(swiftError.Error(), "--timeline is not implemented for Swift") {
		test.Errorf("--timeline was not refused by name for Swift: %v", swiftError)
	}

	if kept := withoutGoOnlySwitches([]string{"--no-fix", "--timeline", "--host-build-only", "--json"}); !slices.Equal(
		kept, []string{"--no-fix", "--json"}) {
		test.Errorf("another engine's child is handed %q, want every flag but the Go-only ones", kept)
	}

	refusal := refuseGoOnlySwitches(map[string]bool{"timeline": true}, typedTrue)
	if refusal == nil || !strings.Contains(refusal.Error(), "--timeline maps when each phase and each build of a Go "+
		"run began and ended, and this is no Go project") {
		test.Errorf("a run at no Go project was refused with %v, want --timeline refused by name", refusal)
	}
	if refusal := refuseGoOnlySwitches(map[string]bool{"timeline": true},
		func(string) string { return "false" }); refusal != nil {
		test.Errorf("--timeline=false asks for nothing, and was refused: %v", refusal)
	}
}

// TestTimelineIsRefusedAtARootWithNoGoProject: discovery hands `--timeline` to a Go project's run alone, so at a root
// of only TypeScript and Swift projects it would reach no run and the run would read as one that mapped what was
// asked. Such a root refuses it before any project runs, while a root holding a Go module takes it.
func TestTimelineIsRefusedAtARootWithNoGoProject(test *testing.T) {
	test.Parallel()
	typeScriptAndSwift := map[string]string{
		"web/tsconfig.json":   discoveryTsconfig,
		"web/source/a.ts":     "export const a = 1;\n",
		"tools/Package.swift": discoveryPackage,
	}
	withGo := map[string]string{"service/go.mod": "module example.com/service\n\ngo 1.27\n"}
	for path, contents := range typeScriptAndSwift {
		withGo[path] = contents
	}
	for name, testCase := range map[string]struct {
		files   map[string]string
		refused bool
	}{
		"TypeScript and Swift":    {files: typeScriptAndSwift, refused: true},
		"a Go module beside them": {files: withGo},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			found, discoverProjectsError := discoverProjects(newRepository(test, testCase.files), nil)
			if discoverProjectsError != nil {
				test.Fatal(discoverProjectsError)
			}
			refuseError := refuseGoOnlySwitchesWithoutGo(found, map[string]bool{"timeline": true}, typedTrue)
			if refused := refuseError != nil; refused != testCase.refused {
				test.Errorf("refused is %v (%v), want %v for the projects %+v", refused, refuseError,
					testCase.refused, found.Projects)
			}
		})
	}
}

// TestARealGoRunUnderTheTimelinePrintsIt: the built binary over a small Go module under `--timeline`, end to end, the
// engine making its recorder as it reads the command line and the front door taking the record because the run asked
// (#3v2zp7g). Under `--json` the timeline line comes before the summary, its host's stamps in their order from the
// engine's start through the host's load to the run's end; without it the table is on the terminal, ending at the
// run's end. No other test reaches the two lines that make this work in a real run: the engine's recorder made in
// runOverlapping, and the front door's run told it asked (runGoEngine).
func TestARealGoRunUnderTheTimelinePrintsIt(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		goModuleMarker: "module example.com/mapped\n\ngo 1.27\n",
		"mapped.go": "// Package mapped is a module with one function to map a run over.\npackage mapped\n\n" +
			"// Mapped is one.\nfunc Mapped() int {\n\treturn 1\n}\n",
	})
	// A finding of a house rule would exit 1 and still be a whole run, so only a run that did not end is refused here.
	output, exit := runCohere(test, binary, root, "--no-fix", "--timeline", "--json")
	if exit != 0 && exit != 1 {
		test.Fatalf("the run exited %d, want a run that ended:\n%s", exit, output)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	timeline := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, `{"kind":"timeline"`) })
	summary := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, `"kind":"summary"`) })
	if timeline < 0 || summary < timeline {
		test.Fatalf("no timeline line before the summary:\n%s", output)
	}
	var record contract.TimelineRecord
	if decodeError := json.Unmarshal([]byte(lines[timeline]), &record); decodeError != nil {
		test.Fatalf("decoding the timeline line %q: %v", lines[timeline], decodeError)
	}
	var labels []string
	for _, stamp := range record.Stamps {
		if slices.Contains([]string{"engine.main.begin", "host.load.end", "run.end"}, stamp.Label) {
			labels = append(labels, stamp.Label)
		}
	}
	if want := []string{"engine.main.begin", "host.load.end", "run.end"}; !slices.Equal(labels, want) {
		test.Errorf("the timeline stamped %q of the host's steps, want %q in that order:\n%s", labels, want,
			lines[timeline])
	}

	output, exit = runCohere(test, binary, root, "--no-fix", "--timeline")
	if exit != 0 && exit != 1 || !strings.Contains(output, "\ntimeline: when each step began or ended") ||
		!strings.Contains(output, "\n  run.end ") {
		test.Errorf("the run without --json exited %d, want a run that ended with the timeline's table through the "+
			"run's end:\n%s",
			exit, output)
	}
}

// TestANarrowedRunOfNoGoProjectRefusesAGoOnlySwitch: a run given `--directory` into a TypeScript project whose folder
// check finds a folder runs the project as a child of its own (checkNarrowedProjectFolders), which is handed no switch
// only a Go run takes, so `--timeline` and `--host-build-only` there would reach no run and the run would read as one
// that did what was asked. Each is refused before the project runs, by name, and the same run without either judges
// the folder, so the refusal is that path's.
func TestANarrowedRunOfNoGoProjectRefusesAGoOnlySwitch(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".gitignore": ".cache/\n",
		"apps/web/CohereSettings.json": "{\n    \"extends\": \"cohere:typescript\",\n    \"rules\": {\n" +
			"        \"" + configuration.AbbreviatedFolderName + "\": \"error\"\n    }\n}\n",
		"apps/web/tsconfig.json": "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, " +
			"\"include\": [\"source\"] }\n",
		"apps/web/source/tidy.ts": "export const tidy = 1;\n",
		"apps/web/docs/README.md": "# Documentation\n",
	})
	folder := filepath.Join(root, "apps", "web", "docs")
	output, exit := runCohere(test, binary, root, "--no-fix", "--directory", "apps/web")
	if exit != 1 || !strings.Contains(output, folder+"/ - error "+configuration.AbbreviatedFolderName) {
		test.Fatalf("the control exited %d, want 1 naming the folder, so the run reaches the folder check:\n%s", exit,
			output)
	}
	for switchName, refusal := range map[string]string{
		"timeline": "--timeline maps when each phase and each build of a Go run began and ended, and this is no " +
			"Go project",
		"host-build-only": "--host-build-only narrows which builds of a Go project's files are checked, and this " +
			"is no Go project",
	} {
		output, exit := runCohere(test, binary, root, "--no-fix", "--directory", "apps/web", "--"+switchName)
		if exit == 0 || !strings.Contains(output, refusal) || strings.Contains(output, folder+"/ - error") {
			test.Errorf("--%s over a TypeScript project with a folder finding exited %d, want it refused before "+
				"the project ran:\n%s", switchName, exit, output)
		}
	}
}

// TestAGoRunPrintsItsTimeline: a Go run under `--timeline` prints the engine's timeline record as a table (#3v2zp7g),
// in the verbose view and the default one alike, since the switch asked for it by name: the unit first, then each
// stamp in the order taken with its wall and the cores busy since the stamp before, a build's indented under its name
// and a step taken again with its round, `-` on the first row, whose wall and CPU start at different moments, and `-`
// where no wall passed. A record of a timed run says its shape once.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoRunPrintsItsTimeline(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimelineStream(test)
	for _, mode := range []outputMode{outputVerbose, outputHuman} {
		output, renderError := renderGoTimeline(test, mode, true, lines)
		if renderError != nil {
			test.Fatalf("%v\n%s", renderError, output)
		}
		requireLines(test, output,
			"\ntimeline: when each step began or ended, as wall time since the engine started, and the cores busy "+
				"since the step before, the process's CPU over the wall between them\n",
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "step", "wall", "cores"),
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "engine.main.begin", "3.0ms", "-"),
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "host.load.end", "403ms", "3.50"),
			"  windows/amd64\n",
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "  load.end", "603ms", "2.50"),
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "  check.analyze.begin, round 1", "603ms", "-"),
			fmt.Sprintf("  %-*s %9s %7s\n", timelineLabelWidth, "run.end", "803ms", "1.50"),
		)
		forbidLines(test, output, "a timed run's shape")
	}

	timed := slices.Clone(lines)
	index := goTimelineIndex(test, timed)
	timed[index] = strings.Replace(timed[index], `{"kind":"timeline",`, `{"kind":"timeline","timed":true,`, 1)
	output, renderError := renderGoTimeline(test, outputHuman, true, timed)
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	if count := strings.Count(output, "a timed run's shape"); count != 1 {
		test.Errorf("a timed run's timeline said its shape %d times, want once:\n%s", count, output)
	}

	unavailable := slices.Clone(lines)
	unavailable[index] = strings.Replace(unavailable[index], `{"kind":"timeline",`,
		`{"kind":"timeline","unavailable":"the process's CPU clock could not be read",`, 1)
	output, renderError = renderGoTimeline(test, outputHuman, true, unavailable)
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	requireLines(test, output, "no CPU this run, because the process's CPU clock could not be read",
		fmt.Sprintf("  %-*s %9s\n", timelineLabelWidth, "host.load.end", "403ms"))
	forbidLines(test, output, "cores\n", "3.50")
}

// TestAGoTimelineRecordIsWrittenThroughUnderJson: under `--json` the record is one `timeline` line, the engine's fields
// as it wrote them, before the summary, and no table.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoTimelineRecordIsWrittenThroughUnderJson(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimelineStream(test)
	output, renderError := renderGoTimeline(test, outputJson, true, lines)
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	written := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	timeline := slices.IndexFunc(written, func(line string) bool {
		return strings.HasPrefix(line, `{"kind":"timeline"`)
	})
	summary := slices.IndexFunc(written, func(line string) bool { return strings.Contains(line, `"kind":"summary"`) })
	if timeline < 0 || summary < timeline {
		test.Fatalf("no timeline line before the summary:\n%s", output)
	}
	// Byte for byte: the fixture is written in the field order the engine encodes, which is the order written through.
	if engineLine := lines[goTimelineIndex(test, lines)]; written[timeline] != engineLine {
		test.Errorf("the timeline line is\n%s\nwant the engine's\n%s", written[timeline], engineLine)
	}
	forbidLines(test, output, "timeline: when each step")
}

// TestABrokenGoTimelineRecordIsRefused: a timeline record where the run did not ask for one, none where it did, a
// second one, a record but the summary after it, one before the last phase's record, and one missing the stamps the
// contract writes `[]`, each break the stream, as every record out of its place does.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestABrokenGoTimelineRecordIsRefused(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimelineStream(test)
	timeline := goTimelineIndex(test, lines)
	inputs := `{"kind":"inputs","files":["/project/go.mod"],"directories":["/project"],"absent":[]}`
	for _, testCase := range []struct {
		name              string
		timelineRequested bool
		lines             []string
		refusal           string
	}{
		{"the control: the record where it belongs, in a run that asked", true, lines, ""},
		{"in a run that did not ask", false, lines,
			"a timeline record in a run that did not ask for one with --timeline"},
		{
			"missing, in a run that asked",
			true,
			slices.Delete(slices.Clone(lines), timeline, timeline+1),
			"a summary with no timeline record, in a run that asked for one with --timeline",
		},
		{"twice", true, slices.Insert(slices.Clone(lines), timeline, lines[timeline]), "a second timeline record"},
		{
			"before the inputs record",
			true,
			slices.Insert(slices.Clone(lines), timeline+1, inputs),
			`a "inputs" record after the timeline record, which is the last before the summary`,
		},
		{
			"before the unused phase's record",
			true,
			slices.Insert(slices.Delete(slices.Clone(lines), timeline, timeline+1), timeline-1, lines[timeline]),
			"a timeline record before the unused phase",
		},
		{
			"with no stamps",
			true,
			slices.Replace(slices.Clone(lines), timeline, timeline+1, `{"kind":"timeline"}`),
			"a timeline record missing its stamps",
		},
	} {
		// Not parallel: renderGoTimeline sets the package-level activeOutput and activeSummary
		test.Run(testCase.name, func(test *testing.T) {
			_, renderError := renderGoTimeline(test, outputVerbose, testCase.timelineRequested, testCase.lines)
			if testCase.refusal == "" {
				if renderError != nil {
					test.Fatalf("the control was refused: %v", renderError)
				}
				return
			}
			if renderError == nil || !strings.Contains(renderError.Error(), testCase.refusal) {
				test.Errorf("refused with %v, want %q", renderError, testCase.refusal)
			}
		})
	}
}
