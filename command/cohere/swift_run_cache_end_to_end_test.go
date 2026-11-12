//go:build unix

// The stand-in engine and the stand-in `swift` these scenarios put on PATH are /bin/sh scripts, so the file builds on
// Unix.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// swiftReplayFixture is a tiny Swift package the real cohere binary checks, through a stand-in engine named by the
// override and a stand-in `swift` first on PATH, so the front door's half of the replay is proved without building the
// engine: which runs replay, what a replay prints, and that every input the front door names, the toolchain and the
// engine among them, moves the replay when it changes. Which files the real engine names is proved by its own tests
// (swift/Tests/CohereSwiftTests/RunInputsTests.swift); this stand-in names every file and directory below the root,
// as the real engine's walk does. Each test has its own, so they run in parallel and none sees another's table.
type swiftReplayFixture struct {
	test   *testing.T
	binary string
	root   string

	// tools is first on PATH and holds the stand-in `swift`, whose `--version` is the toolchain string.
	tools string
	// engine is the stand-in engine the override names.
	engine string
	// starts is a file the engine adds a line to each time it starts, so a test can count the runs that were not
	// replays.
	starts string

	// environment is added to every run's, each "NAME=value" overriding this process's own.
	environment []string
}

// swiftReplayEngine is the stand-in engine: a check run over `--root` that reports one rule finding, at App.swift,
// and names every file and directory below the root but cohere's cache, as the engine's inputs record does.
const swiftReplayEngine = `echo started >> "$COHERE_TEST_SWIFT_STARTS"
root=""
while [ $# -gt 0 ]; do
	if [ "$1" = "--root" ]; then root="$2"; shift; fi
	shift
done
listed() { find "$root" -name .cache -prune -o -type "$1" -print | sort | sed 's/.*/"&"/' | paste -sd, - ; }
echo '{"kind":"provenance","contract":3,"engine":"cohere-swift","version":"0.1.0","commit":"dev","sourceTreeModified":false,"toolchain":"swiftlang-6.4.0.34.1","swiftSyntax":"604.0.0","swiftFormat":"604.0.0"}'
printf '{"kind":"project","root":"%s","package":"App","elapsedMilliseconds":5,"filesInPackage":2,"filesOurs":2,"filesInScope":2,"scopeDescription":"","targets":[{"name":"App","kind":"library","files":2,"languageMode":"6"}],"excluded":[]}\n' "$root"
echo '{"kind":"fix","filesConsidered":2,"filesRewritten":0,"fixesApplied":0,"fixesRefused":0,"refusalsByReason":{},"filesReformatted":0,"filesNotFormatted":0,"notFormattedReasons":{},"formatScope":"every file in the package","changedFiles":[]}'
echo '{"kind":"phase","name":"fix","outcome":"ran","elapsedMilliseconds":3,"detail":""}'
echo '{"kind":"types","diagnostics":0,"files":2,"elapsedMilliseconds":7,"filesWithoutRecord":[],"build":"swift build"}'
echo '{"kind":"phase","name":"types","outcome":"ran","elapsedMilliseconds":7,"detail":""}'
printf '{"kind":"finding","source":"rule","file":"%s/Sources/App/App.swift","line":1,"column":1,"severity":"error","rule":"cohere-swift/force-unwrapping","messageId":"forceUnwrap","message":"force unwrapping crashes on nil","fixes":[],"suggestions":[]}\n' "$root"
echo '{"kind":"lint","findings":1,"rulesRun":4,"filesWalked":2,"nodesVisited":64,"elapsedMilliseconds":2,"reusedFrom":"","rulesSilent":[],"rulesWatchedAndQuiet":3,"crashes":[],"rulesScopedOff":{},"rulesNotConfigured":[],"configNote":""}'
echo '{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":2,"detail":""}'
echo '{"kind":"phase","name":"unused","outcome":"skipped","elapsedMilliseconds":0,"detail":"not requested ` + "\u2014" +
	` this is a report, ask for it with --unused"}'
printf '{"kind":"inputs","files":[%s],"directories":[%s],"absent":["%s/.swift-format"]}\n' "$(listed f)" "$(listed d)" "$root"
echo '{"kind":"summary","findings":1,"complete":true,"exitCode":1}'
exit 1
`

// swiftReplayVersion is what the stand-in `swift` prints for `--version`, toolchain names its toolchain string.
func swiftReplayVersion(toolchain string) string {
	return "echo 'swift-driver version: 1.0 Apple Swift version 6.4 (" + toolchain + " clang-1700.0.0.1)'\n" +
		"echo 'Target: arm64-apple-macosx26.0'\n"
}

func newSwiftReplayFixture(test *testing.T) *swiftReplayFixture {
	test.Helper()
	fixture := &swiftReplayFixture{
		test:   test,
		binary: buildCohere(test),
		root:   filepath.Join(test.TempDir(), "package"),
		tools:  test.TempDir(),
		engine: filepath.Join(test.TempDir(), "cohere-swift"),
		starts: filepath.Join(test.TempDir(), "starts"),
	}
	fixture.write("Package.swift", "// swift-tools-version:6.2\nimport PackageDescription\n")
	fixture.write(".gitignore", ".cache/\n")
	fixture.write("Sources/App/App.swift", "let value: Int? = 1\nlet forced = value!\n")
	fixture.write("Sources/App/Second.swift", "struct Second {}\n")
	fixture.script(fixture.engine, swiftReplayEngine)
	fixture.script(filepath.Join(fixture.tools, "swift"), swiftReplayVersion("swiftlang-6.4.0.34.1"))
	fixture.environment = []string{
		"PATH=" + fixture.tools + string(os.PathListSeparator) + os.Getenv("PATH"),
		"COHERE_SWIFT_ENGINE=" + fixture.engine,
		"COHERE_TEST_SWIFT_STARTS=" + fixture.starts,
	}
	return fixture
}

func (fixture *swiftReplayFixture) write(name string, contents string) {
	fixture.test.Helper()
	path := filepath.Join(fixture.root, name)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		fixture.test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		fixture.test.Fatal(writeError)
	}
}

// script writes an executable /bin/sh script at path.
func (fixture *swiftReplayFixture) script(path string, body string) {
	fixture.test.Helper()
	if writeError := executable_testing.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); writeError != nil {
		fixture.test.Fatal(writeError)
	}
}

// setVariable sets one variable for every run from here on.
func (fixture *swiftReplayFixture) setVariable(name string, value string) {
	fixture.environment = append(withoutVariable(fixture.environment, name), name+"="+value)
}

// variable is the value every run from here on gets for name, empty when the fixture sets none.
func (fixture *swiftReplayFixture) variable(name string) string {
	for _, variable := range slices.Backward(fixture.environment) {
		if value, found := strings.CutPrefix(variable, name+"="); found {
			return value
		}
	}
	return ""
}

// engineStarts is how many times the stand-in engine has started.
func (fixture *swiftReplayFixture) engineStarts() int {
	fixture.test.Helper()
	contents, readError := os.ReadFile(fixture.starts)
	if errors.Is(readError, os.ErrNotExist) {
		return 0
	}
	if readError != nil {
		fixture.test.Fatal(readError)
	}
	return strings.Count(string(contents), "\n")
}

// run launches cohere at the root with arguments, with `--no-cache` added when cached is false: the cold truth every
// cached run is held against.
func (fixture *swiftReplayFixture) run(cached bool, arguments ...string) goReplayRun {
	fixture.test.Helper()
	commandLine := slices.Clone(arguments)
	if !cached {
		commandLine = append(commandLine, "--no-cache")
	}
	command := exec.Command(fixture.binary, commandLine...)
	command.Dir = fixture.root
	command.Env = append(os.Environ(), fixture.environment...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runError := command.Run()
	var exitError *exec.ExitError
	if runError != nil && !errors.As(runError, &exitError) {
		fixture.test.Fatalf("running cohere: %v\n%s%s", runError, stdout.String(), stderr.String())
	}
	exitCode := 0
	if exitError != nil {
		exitCode = childExitCode(fixture.test, exitError)
	}
	return goReplayRun{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

// establishHit runs twice with arguments and requires the second to replay the first without starting the engine,
// since nothing below can show a change was noticed until an unchanged package is shown to replay.
func (fixture *swiftReplayFixture) establishHit(arguments ...string) {
	fixture.test.Helper()
	recorded := fixture.run(true, arguments...)
	started := fixture.engineStarts()
	replayed := fixture.run(true, arguments...)
	if !isSwiftReplay(replayed) || fixture.engineStarts() != started {
		fixture.test.Fatalf("an unchanged package did not replay:\n--- the recording run\n%s%s\n--- the next run\n%s%s",
			recorded.stdout, recorded.stderr, replayed.stdout, replayed.stderr)
	}
}

// isSwiftReplay reports a run the cache answered, as isGoReplay reads one: the framing on its first line, the footer
// saying so under `--verbose`, the summary's `cache.replayed` under `--json`, and in the default view a footer that
// checked no file and found every one cached.
func isSwiftReplay(run goReplayRun) bool {
	return strings.HasPrefix(run.stdout, "cached: ") || goReplayFooter.MatchString(run.stdout) ||
		strings.Contains(run.stdout, `"replayed":true`) || goEveryFileCachedFooter.MatchString(run.stdout)
}

// swiftVerdict is a run's verdict, as goVerdict is a Go run's: its output without the lines that describe the
// invocation, with the Swift project line in place of the Go one.
func swiftVerdict(cold string) string {
	return goKeepLines(goVerdict(cold), "package described in ")
}

// TestASwiftReplayIsTheColdRunsVerdict: with nothing changed, the second run replays the first without starting the
// engine, and what it prints is a `--no-cache` run's verdict byte for byte in every view, with the same exit, framed
// as a Go replay is: `--verbose` once the replay's framing and the cold run's invocation lines are off, `--json` line
// for line and the summary but for what is the invocation's, and the default view but for its footer. A replay never
// says which engine the override named, since it ran none.
func TestASwiftReplayIsTheColdRunsVerdict(test *testing.T) {
	test.Parallel()
	for _, view := range []string{"--verbose", "--json", "the default view"} {
		test.Run(view, func(test *testing.T) {
			test.Parallel()
			fixture := newSwiftReplayFixture(test)
			arguments := []string{"--no-fix"}
			if strings.HasPrefix(view, "--") {
				arguments = append(arguments, view)
			}
			recorded := fixture.run(true, arguments...)
			replayed := fixture.run(true, arguments...)
			cold := fixture.run(false, arguments...)
			if recorded.exitCode != 1 || !strings.Contains(recorded.stdout, "cohere-swift/force-unwrapping") {
				test.Fatalf("the recording run exited %d without the fixture's finding, so it proves nothing:\n%s%s",
					recorded.exitCode, recorded.stdout, recorded.stderr)
			}
			if !isSwiftReplay(replayed) {
				test.Fatalf("the second run did not replay:\n%s%s", replayed.stdout, replayed.stderr)
			}
			if starts := fixture.engineStarts(); starts != 2 {
				test.Errorf("the engine started %d times over a recording, a replay and a cold run, want 2", starts)
			}
			if replayed.exitCode != cold.exitCode {
				test.Errorf("the replay exited %d and the cold run %d", replayed.exitCode, cold.exitCode)
			}
			if strings.Contains(replayed.stderr, "running the Swift engine") {
				test.Errorf("the replay said it ran an engine:\n%s", replayed.stderr)
			}
			coldErrors := goKeepLines(cold.stderr, "cohere: running the Swift engine ")
			if replayed.stderr != coldErrors {
				test.Errorf("stderr differs:\n--- replay\n%s\n--- cold\n%s", replayed.stderr, coldErrors)
			}
			switch view {
			case "--verbose":
				if !goProvenance.MatchString(replayed.stdout) {
					test.Errorf("the replay printed the fix line as though the fix phase had just run:\n%s",
						replayed.stdout)
				}
				if !strings.HasPrefix(replayed.stdout, "cached: ") || !goReplayFooter.MatchString(replayed.stdout) ||
					!strings.Contains(replayed.stdout, "\nphases: replayed the run at ") {
					test.Errorf("the replay is not framed as one, first line to footer:\n%s", replayed.stdout)
				}
				for _, claim := range []string{"package described in ", "types: ", "lint: ", "  total "} {
					if regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(claim)).MatchString(replayed.stdout) {
						test.Errorf("the replay printed the recorded run's %q line as this run's:\n%s", claim,
							replayed.stdout)
					}
				}
				if goReplayBody(replayed.stdout) != swiftVerdict(cold.stdout) {
					test.Fatalf("the replay is not the cold run's verdict:\n--- replay\n%s\n--- cold verdict\n%s",
						goReplayBody(replayed.stdout), swiftVerdict(cold.stdout))
				}
			case "--json":
				replayLines := goKeepLines(replayed.stdout, `{"kind":"summary"`)
				coldLines := goKeepLines(cold.stdout, `{"kind":"summary"`)
				if replayLines != coldLines {
					test.Fatalf("the replay's records differ:\n--- replay\n%s\n--- cold\n%s", replayLines, coldLines)
				}
				replaySummary := jsonText(test, goSummaryVerdict(test, replayed.stdout))
				coldSummary := jsonText(test, goSummaryVerdict(test, cold.stdout))
				if replaySummary != coldSummary {
					test.Fatalf("the replay's summary differs:\n--- replay\n%s\n--- cold\n%s",
						replaySummary, coldSummary)
				}
			default:
				replayBody := goKeepLines(replayed.stdout, "✓ 💎 ", "✗ ☠️ ")
				if replayBody != goKeepLines(cold.stdout, "✓ 💎 ", "✗ ☠️ ") {
					test.Fatalf("the replay is not the cold run's verdict:\n--- replay\n%s\n--- cold\n%s",
						replayed.stdout, cold.stdout)
				}
			}
		})
	}
}

// TestASwiftRunWithAStaleInputIsNeverReplayed: for every kind of input a Swift verdict depends on that reaches the
// record through the front door, a package that replays is changed in that input alone, and the next run starts the
// engine. The engine's own files, a listing and a deletion stand for every path the engine names; the rest are the
// front door's: the settings file it hands the engine, the toolchain string, which a replay never asks for and so is
// held by the `swift` that answered it and by the key's toolchain facts, and the engine's identity.
func TestASwiftRunWithAStaleInputIsNeverReplayed(test *testing.T) {
	test.Parallel()
	scenarios := []struct {
		name   string
		change func(fixture *swiftReplayFixture)
	}{
		{"a source edited", func(fixture *swiftReplayFixture) {
			fixture.write("Sources/App/App.swift", "let value: Int? = 2\nlet forced = value!\n")
		}},
		{"a file added", func(fixture *swiftReplayFixture) {
			fixture.write("Sources/App/Third.swift", "struct Third {}\n")
		}},
		{"a file deleted", func(fixture *swiftReplayFixture) {
			if removeError := os.Remove(filepath.Join(fixture.root, "Sources/App/Second.swift")); removeError != nil {
				fixture.test.Fatal(removeError)
			}
		}},
		{"the settings file created", func(fixture *swiftReplayFixture) {
			fixture.write("CohereSettings.json", `{"swift":{"rules":{}}}`)
		}},
		// The same `swift` in the same place now names another toolchain, as an Xcode update leaves /usr/bin/swift.
		{"the toolchain string changed", func(fixture *swiftReplayFixture) {
			fixture.script(filepath.Join(fixture.tools, "swift"), swiftReplayVersion("swiftlang-6.5.0.1.1"))
		}},
		{"another swift first on PATH", func(fixture *swiftReplayFixture) {
			another := fixture.test.TempDir()
			fixture.script(filepath.Join(another, "swift"), swiftReplayVersion("swiftlang-6.5.0.1.1"))
			fixture.setVariable("PATH", another+string(os.PathListSeparator)+fixture.variable("PATH"))
		}},
		{"DEVELOPER_DIR names another Xcode", func(fixture *swiftReplayFixture) {
			fixture.setVariable("DEVELOPER_DIR", "/Applications/Xcode-beta.app/Contents/Developer")
		}},
		// Rebuilt with the same behavior: only its bytes say it is another engine, and its rules may differ.
		{"the engine rebuilt in place", func(fixture *swiftReplayFixture) {
			fixture.script(fixture.engine, "# rebuilt\n"+swiftReplayEngine)
		}},
		{"another engine named", func(fixture *swiftReplayFixture) {
			another := filepath.Join(fixture.test.TempDir(), "cohere-swift")
			fixture.script(another, swiftReplayEngine)
			fixture.setVariable("COHERE_SWIFT_ENGINE", another)
		}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newSwiftReplayFixture(test)
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			// The new modification time must differ from the recorded one at the file system's resolution.
			time.Sleep(10 * time.Millisecond)
			scenario.change(fixture)
			started := fixture.engineStarts()
			after := fixture.run(true, arguments...)
			if isSwiftReplay(after) || fixture.engineStarts() != started+1 {
				test.Fatalf("%s and the next run replayed the recorded verdict:\n%s%s", scenario.name, after.stdout,
					after.stderr)
			}
		})
	}
}

// TestASwiftInputChangedDuringTheRunIsNeverReplayedOver: a source's bytes, and a file added under a directory the
// engine listed, each changed after the engine read the tree and before the front door recorded its run. That run
// saw the old tree, and its record would sign the new one, so the record is refused by the clock the run took before
// discovery, and the next run checks again and prints what a cold run prints.
func TestASwiftInputChangedDuringTheRunIsNeverReplayedOver(test *testing.T) {
	test.Parallel()
	for _, change := range []struct {
		name string
		path string
		text string
	}{
		{"a source's bytes", "Sources/App/App.swift", "let value: Int? = 3\nlet forced = value!\n"},
		{"a file under a listed directory", "Sources/App/Late/Late.swift", "struct Late {}\n"},
	} {
		test.Run(change.name, func(test *testing.T) {
			test.Parallel()
			fixture := newSwiftReplayFixture(test)
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			// A change of its own first, so the run below is a miss, starts the engine, and is the record in question.
			time.Sleep(10 * time.Millisecond)
			fixture.write("Sources/App/Second.swift", "struct Second { let edited = 1 }\n")
			path := filepath.Join(fixture.root, change.path)
			during := slices.Clone(fixture.environment)
			fixture.setVariable("COHERE_TEST_WRITE_AFTER_SWIFT_ENGINE", path)
			fixture.setVariable("COHERE_TEST_WRITE_AFTER_SWIFT_ENGINE_TEXT", change.text)
			if changed := fixture.run(true, arguments...); isSwiftReplay(changed) {
				test.Fatalf("the run with the change in it replayed, so it proves nothing:\n%s", changed.stdout)
			}
			fixture.environment = during
			if contents, readError := os.ReadFile(path); readError != nil || string(contents) != change.text {
				test.Fatalf("the instrument did not write %s, so nothing below is about it: %v", path, readError)
			}

			started := fixture.engineStarts()
			after := fixture.run(true, arguments...)
			cold := fixture.run(false, arguments...)
			if isSwiftReplay(after) || fixture.engineStarts() != started+2 {
				test.Fatalf("a run recorded over a change made while it ran was replayed:\n%s", after.stdout)
			}
			if goNormalized(swiftVerdict(after.stdout)) != goNormalized(swiftVerdict(cold.stdout)) ||
				after.exitCode != cold.exitCode {
				test.Fatalf("the run after the change differs from a cold run:\n--- after\n%s\n--- cold\n%s",
					after.stdout, cold.stdout)
			}
		})
	}
}

// TestASwiftRunUnderNoCacheNeitherReplaysNorRecords: on a package whose run replays, `--no-cache` starts the engine,
// says its cache was off, and leaves the cache table exactly as it found it; and a `--no-cache` run on a package with
// no table records nothing, so the run after it starts the engine too.
func TestASwiftRunUnderNoCacheNeitherReplaysNorRecords(test *testing.T) {
	test.Parallel()
	fresh := newSwiftReplayFixture(test)
	fresh.run(false, "--no-fix", "--verbose")
	if after := fresh.run(true, "--no-fix", "--verbose"); isSwiftReplay(after) || fresh.engineStarts() != 2 {
		test.Fatalf("a run after a --no-cache run replayed, so the --no-cache run was recorded:\n%s", after.stdout)
	}

	fixture := newSwiftReplayFixture(test)
	fixture.establishHit("--no-fix", "--verbose")
	fixture.establishHit("--no-fix", "--json")
	before := cacheDirectoryState(test, fixture.root)
	for _, view := range []string{"--verbose", "--json"} {
		started := fixture.engineStarts()
		cold := fixture.run(false, "--no-fix", view)
		if isSwiftReplay(cold) || fixture.engineStarts() != started+1 {
			test.Fatalf("a --no-cache run replayed:\n%s", cold.stdout)
		}
		switch view {
		case "--verbose":
			if !strings.Contains(cold.stdout, "  cache: off, by --no-cache: nothing was read from or written to "+
				"this package's cache: no recorded run was replayed") {
				test.Errorf("the account does not say the cache was off:\n%s", cold.stdout)
			}
		case "--json":
			var summary struct {
				Cache struct {
					Off      bool `json:"off"`
					Replayed bool `json:"replayed"`
				} `json:"cache"`
			}
			if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, cold.stdout)), &summary); decodeError != nil {
				test.Fatal(decodeError)
			}
			if !summary.Cache.Off || summary.Cache.Replayed {
				test.Errorf("the summary's cache reads off %t, replayed %t:\n%s", summary.Cache.Off,
					summary.Cache.Replayed, cold.stdout)
			}
		}
	}
	if after := cacheDirectoryState(test, fixture.root); after != before {
		test.Errorf("a --no-cache run wrote the cache table:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// TestEveryProjectOfARootWithGoAndSwiftReplays: a root holding a Go module and a Swift package below it, as cohere's
// own does, checks each as a child of the run, and a later run replays both, the Swift one without starting its
// engine. A Swift child judged by its parent's command line is what lets a bare run at such a root replay at all.
func TestEveryProjectOfARootWithGoAndSwiftReplays(test *testing.T) {
	test.Parallel()
	swift := newSwiftReplayFixture(test)
	root := newRepository(test, map[string]string{
		".gitignore":                  ".cache/\n",
		"go.mod":                      "module example.com/service\n\ngo 1.27\n",
		"go.sum":                      "",
		"a.go":                        goReplaySource("service", "Greet"),
		"CohereSettings.json":         `{"go":{"rules":{}}}`,
		"swift/Package.swift":         "// swift-tools-version:6.2\nimport PackageDescription\n",
		"swift/Sources/App/App.swift": "let value: Int? = 1\nlet forced = value!\n",
	})
	run := func() string {
		test.Helper()
		command := exec.Command(swift.binary, "--no-fix", "--verbose")
		command.Dir = root
		command.Env = append(append(withoutVariable(os.Environ(), goEngineChildVariable), "COHERE_VERDICT_FD="),
			swift.environment...)
		output, runError := command.CombinedOutput()
		var exitError *exec.ExitError
		if runError != nil && !errors.As(runError, &exitError) {
			test.Fatalf("running cohere: %v\n%s", runError, output)
		}
		return string(output)
	}
	sections := func(output string) map[string]string {
		found := map[string]string{}
		heading := regexp.MustCompile(`(?m)^== (\S+) \((Go|Swift)\) ==\n`)
		bounds := heading.FindAllStringSubmatchIndex(output, -1)
		for index, bound := range bounds {
			end := len(output)
			if index+1 < len(bounds) {
				end = bounds[index+1][0]
			}
			found[output[bound[4]:bound[5]]] = output[bound[1]:end]
		}
		return found
	}

	// The first run makes each project's cache directory, and the Swift one's moves the listing of `swift/`, which
	// the Go engine read, so that run's Go verdict is not recorded; the next one is, and the one after replays both.
	first := run()
	if len(sections(first)) != 2 || swift.engineStarts() != 1 {
		test.Fatalf("the root was not checked as a Go and a Swift project:\n%s", first)
	}
	run()
	second := sections(run())
	for _, engine := range []string{"Go", "Swift"} {
		if section, found := second[engine]; !found || !strings.HasPrefix(section, "cached: ") {
			test.Errorf("the %s project ran again rather than replaying:\n%s", engine, section)
		}
	}
	if starts := swift.engineStarts(); starts != 1 {
		test.Errorf("the Swift engine started %d times over a recording and two replays, want 1", starts)
	}
}

// swiftScriptsRootEngine is the stand-in engine at a scripts-only root (#wpd36zj): a check over `--root`'s Swift
// files, one rule finding at each that holds `forced`, whose inputs record names what the real engine's names there
// (swift/Tests/CohereSwiftTests/StandaloneScriptTests.swift, aScriptsOnlyRootsRecordNamesWhatDecidesItsVerdict):
// each Swift file and each directory its walk listed, and nothing under node_modules, .cache or .git, which the walk
// never enters. COHERE_TEST_WALK_DEPENDENCIES names node_modules too, the walk a mutant would make.
const swiftScriptsRootEngine = `echo started >> "$COHERE_TEST_SWIFT_STARTS"
root=""
while [ $# -gt 0 ]; do
	if [ "$1" = "--root" ]; then root="$2"; shift; fi
	shift
done
skipped='-name node_modules -o -name .cache -o -name .git'
if [ -n "$COHERE_TEST_WALK_DEPENDENCIES" ]; then skipped='-name .cache -o -name .git'; fi
listed() { find "$root" \( $skipped \) -prune -o -type "$1" $2 -print | sort | sed 's/.*/"&"/' | paste -sd, - ; }
scripts=$(find "$root" \( -name node_modules -o -name .cache -o -name .git \) -prune -o -type f -name '*.swift' -print | sort)
count=$(printf '%s\n' "$scripts" | grep -c .)
echo '{"kind":"provenance","contract":3,"engine":"cohere-swift","version":"0.1.0","commit":"dev","sourceTreeModified":false,"toolchain":"swiftlang-6.4.0.34.1","swiftSyntax":"604.0.0","swiftFormat":"604.0.0"}'
printf '{"kind":"project","root":"%s","package":"root","elapsedMilliseconds":5,"filesInPackage":%d,"filesOurs":%d,"filesInScope":%d,"scopeDescription":"","targets":[],"excluded":[],"projectKind":"Scripts"}\n' "$root" "$count" "$count" "$count"
printf '{"kind":"fix","filesConsidered":%d,"filesRewritten":0,"fixesApplied":0,"fixesRefused":0,"refusalsByReason":{},"filesReformatted":0,"filesNotFormatted":0,"notFormattedReasons":{},"formatScope":"every file in the package","changedFiles":[]}\n' "$count"
echo '{"kind":"phase","name":"fix","outcome":"ran","elapsedMilliseconds":3,"detail":""}'
printf '{"kind":"types","diagnostics":0,"files":%d,"elapsedMilliseconds":7,"filesWithoutRecord":[],"build":"scripts checked alone"}\n' "$count"
echo '{"kind":"phase","name":"types","outcome":"ran","elapsedMilliseconds":7,"detail":""}'
findings=0
for script in $scripts; do
	if grep -q forced "$script"; then
		findings=$((findings + 1))
		printf '{"kind":"finding","source":"rule","file":"%s","line":1,"column":1,"severity":"error","rule":"cohere-swift/force-unwrapping","messageId":"forceUnwrap","message":"force unwrapping crashes on nil","fixes":[],"suggestions":[]}\n' "$script"
	fi
done
printf '{"kind":"lint","findings":%d,"rulesRun":4,"filesWalked":%d,"nodesVisited":64,"elapsedMilliseconds":2,"reusedFrom":"","rulesSilent":[],"rulesWatchedAndQuiet":3,"crashes":[],"rulesScopedOff":{},"rulesNotConfigured":[],"configNote":""}\n' "$findings" "$count"
echo '{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":2,"detail":""}'
echo '{"kind":"phase","name":"unused","outcome":"skipped","elapsedMilliseconds":0,"detail":"not requested ` + "\u2014" +
	` this is a report, ask for it with --unused"}'
printf '{"kind":"inputs","files":[%s],"directories":[%s],"absent":["%s/Package.swift"]}\n' "$(listed f "-name *.swift")" "$(listed d)" "$root"
exitCode=0; if [ "$findings" -gt 0 ]; then exitCode=1; fi
printf '{"kind":"summary","findings":%d,"complete":true,"exitCode":%d}\n' "$findings" "$exitCode"
exit $exitCode
`

// TestAScriptsOnlyRootReplaysUntilAScriptMoves: a scripts-only root's run, started as discovery starts it (its
// directory and its engine named), replays while nothing its record names moves (#wpd36zj). A script edited, and a
// script added beside the others, each make the next run start the engine and report the new finding; a file under
// node_modules changing replays, since the walk never enters it. Under a stand-in that names node_modules, as the
// walk of the whole root would, that change misses too, the cost the engine's record spares.
func TestAScriptsOnlyRootReplaysUntilAScriptMoves(test *testing.T) {
	test.Parallel()
	scenarios := []struct {
		name      string
		walksDeps bool
		change    func(fixture *swiftReplayFixture)
		replays   bool
		finding   string
	}{
		{"a script edited", false, func(fixture *swiftReplayFixture) {
			fixture.write("modules/ebay/EbayVision.swift", "let forced = 1\nprint(forced)\n")
		}, false, "modules/ebay/EbayVision.swift:1:1"},
		{"a script added beside them", false, func(fixture *swiftReplayFixture) {
			fixture.write("modules/ebay/Second.swift", "let forced = 2\nprint(forced)\n")
		}, false, "modules/ebay/Second.swift:1:1"},
		{"a dependency's Swift changed", false, func(fixture *swiftReplayFixture) {
			fixture.write("node_modules/dependency/Tool.swift", "print(2)\n")
		}, true, ""},
		{"a dependency's Swift changed, the walk entering node_modules", true, func(fixture *swiftReplayFixture) {
			fixture.write("node_modules/dependency/Tool.swift", "print(2)\n")
		}, false, ""},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newSwiftReplayFixture(test)
			fixture.root = filepath.Join(test.TempDir(), "repository")
			fixture.write(".git/info/exclude", "")
			fixture.write(".gitignore", ".cache/\n")
			fixture.write("modules/ebay/EbayVision.swift", "let words = 1\nprint(words)\n")
			fixture.write("node_modules/dependency/Tool.swift", "print(1)\n")
			fixture.script(fixture.engine, swiftScriptsRootEngine)
			fixture.setVariable(projectEngineVariable, string(engineSwift))
			if scenario.walksDeps {
				fixture.setVariable("COHERE_TEST_WALK_DEPENDENCIES", "1")
			}
			// As runProject starts a discovered project: its directory first, which the run cache takes off
			// (runCacheArguments).
			arguments := []string{"--directory", fixture.root, "--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			// The new modification time must differ from the recorded one at the file system's resolution.
			time.Sleep(10 * time.Millisecond)
			scenario.change(fixture)
			started := fixture.engineStarts()
			after := fixture.run(true, arguments...)
			if replayed := isSwiftReplay(after) && fixture.engineStarts() == started; replayed != scenario.replays {
				test.Fatalf("%s: replayed %t, want %t:\n%s%s", scenario.name, replayed, scenario.replays,
					after.stdout, after.stderr)
			}
			if scenario.finding != "" && !strings.Contains(after.stdout, scenario.finding) {
				test.Errorf("%s and the next run did not report %s:\n%s", scenario.name, scenario.finding, after.stdout)
			}
		})
	}
}
