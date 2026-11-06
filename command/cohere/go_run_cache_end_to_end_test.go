//go:build unix

// The stand-in go one scenario puts on PATH is a /bin/sh script, so the file builds on Unix.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// goReplayFixture is a tiny Go module, in a directory of its own so an ancestor go.work can be planted beside it,
// that the real cohere binary checks: a front door, and itself as the Go engine. Each test has its own, so they run
// in parallel and none sees another's cache table.
type goReplayFixture struct {
	test   *testing.T
	binary string

	// parent holds root and nothing else, so a go.work created here is an ancestor's, root's own directory's parent.
	parent string
	root   string

	// environment is added to every run's, each "NAME=value" overriding this process's own. Every run starts with
	// cgo off, so a scenario can turn it on, and with the go command's settings file at a path of the fixture's own
	// where there is none yet, so a scenario can create it.
	environment []string
}

// goReplayFinding is a line the printf rule reports, the finding every planted file carries, so a run that loaded
// the file says so in its verdict.
const goReplayFinding = "\treturn fmt.Sprintf(\"%d\", \"text\")\n"

func newGoReplayFixture(test *testing.T) *goReplayFixture {
	test.Helper()
	parent := test.TempDir()
	fixture := &goReplayFixture{
		test:        test,
		binary:      buildCohere(test),
		parent:      parent,
		root:        filepath.Join(parent, "module"),
		environment: []string{"CGO_ENABLED=0", "GOENV=" + filepath.Join(parent, "go.env")},
	}
	fixture.write("go.mod", "module example.com/replay\n\ngo 1.27\n")
	fixture.write("go.sum", "")
	fixture.write(".gitignore", ".cache/\n")
	fixture.write("CohereSettings.json", `{"go":{"rules":{}}}`)
	fixture.write("a.go", goReplaySource("replay", "Greet"))
	fixture.write("sub/b.go", "package sub\n\n// Answer answers.\nfunc Answer() int {\n\treturn 42\n}\n")
	// A directory `./...` reaches that holds no Go file, so only its listing says when a package appears below it.
	fixture.write("notes/README.md", "notes\n")
	// A file the package embeds, which no Go file names but its directive.
	fixture.write("embedded.go", "package replay\n\nimport _ \"embed\"\n\n// Message is embedded.\n//\n"+
		"//go:embed data/message.txt\nvar Message string\n")
	fixture.write("data/message.txt", "hello\n")
	// Files no load reads until a scenario's environment builds them, each with a finding so the verdict says when
	// one is loaded.
	fixture.write("tagged.go", "//go:build replaytest\n\n"+goReplaySource("replay", "Tagged"))
	fixture.write("only_linux.go", goReplaySource("replay", "OnLinux"))
	fixture.write("with_cgo.go", "//go:build cgo\n\n"+goReplaySource("replay", "WithCgo"))
	return fixture
}

// goReplaySource is a Go file of package name declaring function, whose body the printf rule reports.
func goReplaySource(name string, function string) string {
	return "package " + name + "\n\nimport \"fmt\"\n\n// " + function + " is a fixture.\nfunc " + function +
		"() string {\n" + goReplayFinding + "}\n"
}

func (fixture *goReplayFixture) write(name string, contents string) {
	fixture.test.Helper()
	path := filepath.Join(fixture.root, name)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		fixture.test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		fixture.test.Fatal(writeError)
	}
}

// setVariable sets one variable for every run from here on.
func (fixture *goReplayFixture) setVariable(name string, value string) {
	fixture.environment = append(withoutVariable(fixture.environment, name), name+"="+value)
}

// goReplayRun is what one run printed to each stream, and how it exited.
type goReplayRun struct {
	stdout   string
	stderr   string
	exitCode int
}

// run launches cohere at the root with arguments, with `--no-cache` added when cached is false: the cold truth every
// cached run is held against.
func (fixture *goReplayFixture) run(cached bool, arguments ...string) goReplayRun {
	fixture.test.Helper()
	if !cached {
		arguments = append(slices.Clone(arguments), "--no-cache")
	}
	command := exec.Command(fixture.binary, arguments...)
	command.Dir = fixture.root
	command.Env = append(withoutVariable(os.Environ(), goEngineChildVariable), fixture.environment...)
	var stdout, stderr bytes.Buffer
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

// establishHit runs twice with arguments and requires the second to replay the first, since nothing below can
// show a change was noticed until an unchanged tree is shown to replay.
func (fixture *goReplayFixture) establishHit(arguments ...string) {
	fixture.test.Helper()
	recorded := fixture.run(true, arguments...)
	if replayed := fixture.run(true, arguments...); !isGoReplay(replayed) {
		fixture.test.Fatalf("an unchanged module did not replay:\n--- the recording run\n%s%s\n--- the next run\n%s%s",
			recorded.stdout, recorded.stderr, replayed.stdout, replayed.stderr)
	}
}

// isGoReplay reports a run the cache answered, by what the run itself says: the framing on its first line and the
// footer saying so under `--verbose`, the summary's `cache.replayed` under `--json`, and in the default view a
// footer that checked no file and found every one cached, which only a replay prints for a Go run. A replay never
// starts the engine, so this is also the proof that none was started: a run of the engine checks its files.
func isGoReplay(run goReplayRun) bool {
	return strings.HasPrefix(run.stdout, "cached: ") || goReplayFooter.MatchString(run.stdout) ||
		strings.Contains(run.stdout, `"replayed":true`) || goEveryFileCachedFooter.MatchString(run.stdout)
}

// goReplayFooter is the footer a `--verbose` replay renders of its own, which says it was replayed.
var goReplayFooter = regexp.MustCompile(`(?m)^(✓ 💎|✗ ☠️) [0-9.]+s(?: •[^\n]*)? • replayed`)

// goEveryFileCachedFooter is the default view's footer when no file was checked and every one was cached.
var goEveryFileCachedFooter = regexp.MustCompile(`(?m)^(✓ 💎|✗ ☠️) [0-9.]+s •.*\(\d+ rules • \d+ cached\)$`)

// goKeepLines is output without each line starting with one of drop.
func goKeepLines(output string, drop ...string) string {
	kept := []string{}
	for _, line := range strings.Split(output, "\n") {
		if !slices.ContainsFunc(drop, func(prefix string) bool { return strings.HasPrefix(line, prefix) }) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// goVerdict is a run's verdict: its output without the lines that describe the invocation rather than the tree,
// which a replay deliberately does not print and a cold run does, and without the footer, which is the
// invocation's too. A replay's own framing comes off in goReplayBody. These are the lines the TypeScript run
// cache's end-to-end test drops, with the Go project line, for a module or a workspace, in place of the graph line.
func goVerdict(cold string) string {
	return goKeepLines(cold,
		"module described in ",
		"workspace described in ",
		"types: ",
		"lint: ",
		"phases: ",
		"  total ",
		"  memory: ",
		"  cache: off, by --no-cache",
		"cache: not replayed whole",
		"✓ 💎 ",
		"✗ ☠️ ",
	)
}

// goProvenance is the label a replay gives a phase's line, which comes off before a comparison.
var goProvenance = regexp.MustCompile(`(?m)^(fix|format scope) \(from the cached run at \d\d:\d\d:\d\d\): `)

// goReplayBody is a replay's output without its framing and its footer, each phase line's label taken off.
func goReplayBody(replay string) string {
	return goProvenance.ReplaceAllString(goKeepLines(replay,
		"cached: ",
		"phases: replayed ",
		"  this run: ",
		"  memory: ",
		"✓ 💎 ",
		"✗ ☠️ ",
	), "$1: ")
}

// goDurations are the numbers a run measures of itself, which two runs of one tree never share.
var goDurations = regexp.MustCompile(`\d+(\.\d+)?(ms|s|µs| GB)\b`)

// goNormalized is a fresh run's output as any other fresh run of the same tree prints it: its verdict, durations
// taken out of what is left.
func goNormalized(output string) string {
	return goDurations.ReplaceAllString(goVerdict(output), "T")
}

// goSummaryVerdict is a `--json` run's summary line without what is the invocation's: its time, its phases and
// their times, what the cache did, and what it checked fresh against what the cache answered for.
func goSummaryVerdict(test *testing.T, output string) map[string]any {
	test.Helper()
	var summary map[string]any
	if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, output)), &summary); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, output)
	}
	for _, invocation := range []string{
		"seconds", "graphSeconds", "formattingSeconds", "phases", "cache", "checked", "cached", "nodes",
	} {
		delete(summary, invocation)
	}
	return summary
}

// TestAGoReplayIsTheColdRunsVerdict: with nothing changed, the second run replays the first without starting the
// engine, and what it prints is a `--no-cache` run's verdict byte for byte in every view, with the same exit:
// `--verbose` once the replay's framing and the cold run's invocation lines are off, as the TypeScript run cache's
// test compares them; `--json` line for line, the summary but for what is the invocation's; and the default view
// but for the footer, which is the invocation's and says it was replayed.
func TestAGoReplayIsTheColdRunsVerdict(test *testing.T) {
	test.Parallel()
	for _, view := range []string{"--verbose", "--json", "the default view"} {
		test.Run(view, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			arguments := []string{"--no-fix"}
			if strings.HasPrefix(view, "--") {
				arguments = append(arguments, view)
			}
			recorded := fixture.run(true, arguments...)
			replayed := fixture.run(true, arguments...)
			cold := fixture.run(false, arguments...)
			if recorded.exitCode != 1 || !strings.Contains(recorded.stdout, "cohere-go/printf") {
				test.Fatalf("the recording run exited %d without the fixture's finding, so it proves nothing:\n%s%s",
					recorded.exitCode, recorded.stdout, recorded.stderr)
			}
			if !isGoReplay(replayed) {
				test.Fatalf("the second run did not replay:\n%s%s", replayed.stdout, replayed.stderr)
			}
			if replayed.exitCode != cold.exitCode {
				test.Errorf("the replay exited %d and the cold run %d", replayed.exitCode, cold.exitCode)
			}
			if replayed.stderr != cold.stderr {
				test.Errorf("stderr differs:\n--- replay\n%s\n--- cold\n%s", replayed.stderr, cold.stderr)
			}
			switch view {
			case "--verbose":
				if !goProvenance.MatchString(replayed.stdout) {
					test.Errorf("the replay printed the fix line as though the fix phase had just run:\n%s",
						replayed.stdout)
				}
				// Every line that describes the invocation is the replay's own or left out, never the recorded run's.
				if !strings.HasPrefix(replayed.stdout, "cached: ") || !goReplayFooter.MatchString(replayed.stdout) ||
					!strings.Contains(replayed.stdout, "\nphases: replayed the run at ") {
					test.Errorf("the replay is not framed as one, first line to footer:\n%s", replayed.stdout)
				}
				for _, claim := range []string{
					"module described in ", "workspace described in ", "types: ", "lint: ", "  total ",
				} {
					if regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(claim)).MatchString(replayed.stdout) {
						test.Errorf("the replay printed the recorded run's %q line as this run's:\n%s", claim,
							replayed.stdout)
					}
				}
				if goReplayBody(replayed.stdout) != goVerdict(cold.stdout) {
					test.Fatalf("the replay is not the cold run's verdict:\n--- replay\n%s\n--- cold verdict\n%s",
						goReplayBody(replayed.stdout), goVerdict(cold.stdout))
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

// jsonText is a value as JSON, its keys sorted, so two decoded summaries compare as text.
func jsonText(test *testing.T, value any) string {
	test.Helper()
	encoded, encodeError := json.Marshal(value)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return string(encoded)
}

// TestAGoRunWithAStaleInputIsNeverReplayed: for every kind of input a Go verdict depends on, a module that replays
// is changed in that input alone, and the next run is not a replay and prints what a cold run prints. One
// scenario per kind, since each reaches the record by its own path: the engine's files, its listed directories,
// its absent paths, the settings file the front door declares, and the key's environment and toolchain.
func TestAGoRunWithAStaleInputIsNeverReplayed(test *testing.T) {
	test.Parallel()
	scenarios := []struct {
		name   string
		change func(fixture *goReplayFixture)
	}{
		// The same size, so only the bytes and the modification time moved, as an editor's save of one character.
		{"a .go file's bytes edited in place", func(fixture *goReplayFixture) {
			// The new modification time must differ from the recorded one at the file system's resolution.
			time.Sleep(10 * time.Millisecond)
			fixture.write("a.go", strings.Replace(goReplaySource("replay", "Greet"), `"%d"`, `"%s"`, 1))
		}},
		// The same size and the old modification time, as cp -p, rsync -t and touch -r leave a file: only the change
		// time and the bytes moved.
		{"a same-size edit with its modification time restored", func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.root, "a.go")
			information, statError := os.Stat(path)
			if statError != nil {
				fixture.test.Fatal(statError)
			}
			time.Sleep(10 * time.Millisecond)
			fixture.write("a.go", strings.Replace(goReplaySource("replay", "Greet"), `"%d"`, `"%s"`, 1))
			restored := information.ModTime()
			if changeTimesError := os.Chtimes(path, restored, restored); changeTimesError != nil {
				fixture.test.Fatal(changeTimesError)
			}
		}},
		{"an embedded file edited", func(fixture *goReplayFixture) {
			fixture.write("data/message.txt", "hello, again\n")
		}},
		// Its constraint taken off, so a file the load left out joins it.
		{"a build-tagged file edited", func(fixture *goReplayFixture) {
			fixture.write("tagged.go", goReplaySource("replay", "Tagged"))
		}},
		{"a .go file added to a package directory", func(fixture *goReplayFixture) {
			fixture.write("sub/c.go", goReplaySource("sub", "Added"))
		}},
		// Below a directory that holds no Go file, so no file the load read sits beside it, and only the listed
		// directory's signature moves.
		{"a package directory added", func(fixture *goReplayFixture) {
			fixture.write("notes/fresh/f.go", goReplaySource("fresh", "Fresh"))
		}},
		{"go.mod edited", func(fixture *goReplayFixture) {
			fixture.write("go.mod", "module example.com/replay\n\ngo 1.27\n\n// edited\n")
		}},
		{"go.sum edited", func(fixture *goReplayFixture) {
			fixture.write("go.sum", "example.com/other v1.0.0/go.mod h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=\n")
		}},
		{"a go.work created in an ancestor directory", func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.parent, "go.work")
			if writeError := os.WriteFile(path, []byte("go 1.27\n\nuse ./module\n"), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		// The variable naming it is the same before and after, so only the file's absence, recorded, can tell.
		{"the go command's settings file created", func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.parent, "go.env")
			if writeError := os.WriteFile(path, []byte("GOFLAGS=-tags=replaytest\n"), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		{"the settings file edited", func(fixture *goReplayFixture) {
			fixture.write("CohereSettings.json", `{"go":{"rules":{"cohere-go/printf":"off"}}}`)
		}},
		{"GOFLAGS changed", func(fixture *goReplayFixture) {
			fixture.setVariable("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -tags=replaytest"))
		}},
		// A target other than the host, so only_linux.go joins the load or leaves it.
		{"GOOS changed", func(fixture *goReplayFixture) {
			target := "linux"
			if goHostIsLinux() {
				target = "darwin"
			}
			fixture.setVariable("GOOS", target)
		}},
		{"CGO_ENABLED changed", func(fixture *goReplayFixture) { fixture.setVariable("CGO_ENABLED", "1") }},
		// A directory of its own holding a go that runs the real one, first on PATH, so the cold run still loads.
		{"the go on PATH changed", func(fixture *goReplayFixture) {
			realGo, lookPathError := exec.LookPath("go")
			if lookPathError != nil {
				fixture.test.Fatal(lookPathError)
			}
			toolchain := fixture.test.TempDir()
			script := "#!/bin/sh\nexec '" + realGo + "' \"$@\"\n"
			if writeError := os.WriteFile(filepath.Join(toolchain, "go"), []byte(script), 0o755); writeError != nil {
				fixture.test.Fatal(writeError)
			}
			fixture.setVariable("PATH", toolchain+string(os.PathListSeparator)+os.Getenv("PATH"))
		}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			scenario.change(fixture)
			afterChange := fixture.run(true, arguments...)
			cold := fixture.run(false, arguments...)
			if isGoReplay(afterChange) {
				test.Fatalf("the change was not noticed: the recorded run was replayed as current:\n%s",
					afterChange.stdout)
			}
			if goNormalized(afterChange.stdout) != goNormalized(cold.stdout) || afterChange.exitCode != cold.exitCode {
				test.Fatalf("the run after the change differs from a cold run (exit %d against %d):\n"+
					"--- after the change\n%s\n--- cold\n%s", afterChange.exitCode, cold.exitCode,
					afterChange.stdout, cold.stdout)
			}
			// And the run after that replays the new truth, so the change was recorded and not only missed.
			next := fixture.run(true, arguments...)
			replayedTruth := goReplayBody(next.stdout) == goVerdict(cold.stdout) && next.exitCode == cold.exitCode
			if !isGoReplay(next) || !replayedTruth {
				test.Fatalf("the run after that did not replay the new truth (exit %d against %d):\n--- next\n%s\n"+
					"--- cold verdict\n%s", next.exitCode, cold.exitCode, goReplayBody(next.stdout),
					goVerdict(cold.stdout))
			}
		})
	}
}

// goHostIsLinux reports whether the tests run on Linux, so the GOOS scenario names a target other than the host.
func goHostIsLinux() bool {
	output, outputError := exec.Command("go", "env", "GOHOSTOS").Output()
	return outputError == nil && strings.TrimSpace(string(output)) == "linux"
}

// TestAGoRunUnderNoCacheNeitherReplaysNorRecords: on a module whose run replays, `--no-cache` checks it fresh, says
// its cache was off in the account and in `--json`, and leaves the cache table exactly as it found it.
func TestAGoRunUnderNoCacheNeitherReplaysNorRecords(test *testing.T) {
	test.Parallel()
	fixture := newGoReplayFixture(test)
	fixture.establishHit("--no-fix", "--verbose")
	fixture.establishHit("--no-fix", "--json")
	before := cacheDirectoryState(test, fixture.root)

	for _, view := range []string{"--verbose", "--json"} {
		cold := fixture.run(false, "--no-fix", view)
		if isGoReplay(cold) {
			test.Fatalf("a --no-cache run replayed:\n%s", cold.stdout)
		}
		if cold.exitCode != 1 || !strings.Contains(cold.stdout, "cohere-go/printf") {
			test.Fatalf("a --no-cache run exited %d without the fixture's finding, so the engine did not check it:\n"+
				"%s%s", cold.exitCode, cold.stdout, cold.stderr)
		}
		switch view {
		case "--verbose":
			if !strings.Contains(cold.stdout, "  cache: off, by --no-cache: no recorded run was replayed") {
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

// cacheDirectoryState is every file in the root's cache directory with its size and modification time, one per
// line, so a run that wrote any of them reads differently. The lock is left out: taking it writes nothing.
func cacheDirectoryState(test *testing.T, root string) string {
	test.Helper()
	entries, readError := os.ReadDir(cacheDirectory(root))
	if readError != nil {
		test.Fatal(readError)
	}
	var state strings.Builder
	for _, entry := range entries {
		if entry.Name() == "table.lock" {
			continue
		}
		information, informationError := entry.Info()
		if informationError != nil {
			test.Fatal(informationError)
		}
		fmt.Fprintf(&state, "%s %d bytes, modified %d\n", entry.Name(), information.Size(),
			information.ModTime().UnixNano())
	}
	return state.String()
}

// TestAnUnchangedGoModuleReplays: two runs in a row with nothing touched, the second a replay, on the fixture, on a
// module with no settings file at all, whose absence is recorded as absent and never as a file the run read
// (#e0tybvv refused every such record), and on a module with a staticcheck.conf above it, after a file appears
// beside that one. A key or a record so broad it never hits passes every staleness test, so this is the other half.
func TestAnUnchangedGoModuleReplays(test *testing.T) {
	test.Parallel()
	for _, shape := range []string{"the fixture", "no settings file", "a file added above the module"} {
		test.Run(shape, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			switch shape {
			case "no settings file":
				if removeError := os.Remove(filepath.Join(fixture.root, "CohereSettings.json")); removeError != nil {
					test.Fatal(removeError)
				}
			case "a file added above the module":
				conf := filepath.Join(fixture.parent, "staticcheck.conf")
				if writeError := os.WriteFile(conf, []byte("checks = [\"inherit\"]\n"), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			if shape == "a file added above the module" {
				// Beside the staticcheck.conf the engine named, in a directory whose listing no load reads.
				sibling := filepath.Join(fixture.parent, "sibling.txt")
				if writeError := os.WriteFile(sibling, []byte("beside\n"), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
				if again := fixture.run(true, arguments...); !isGoReplay(again) {
					test.Fatalf("a file added beside the module kept the next run from replaying:\n%s", again.stdout)
				}
			}
		})
	}
}

// TestAGoInputChangedDuringTheRunIsNeverReplayedOver: an input that changes after the run began reading it, here
// after the engine read the tree and before the run is recorded, keeps the run from being recorded, so the next
// run checks it rather than replaying a verdict that never saw the change. The clock starts before the engine is
// spawned, and every recorded file and listed directory is held to it: a file's bytes edited, and a package created
// under a directory holding no Go file, whose listing is the only thing that moved.
func TestAGoInputChangedDuringTheRunIsNeverReplayedOver(test *testing.T) {
	test.Parallel()
	for _, change := range []struct {
		name string
		path string
		text string
	}{
		{"a file's bytes", "a.go", strings.Replace(goReplaySource("replay", "Greet"), `"%d"`, `"%s"`, 1)},
		{"a package under a listed directory", "notes/late/late.go", goReplaySource("late", "Late")},
	} {
		test.Run(change.name, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			// A change of its own first, so the run below is a miss, runs the engine, and is the record in question.
			fixture.write("sub/b.go", "package sub\n\n// Answer answers.\nfunc Answer() int {\n\treturn 43\n}\n")
			path := filepath.Join(fixture.root, change.path)
			during := fixture.environment
			fixture.environment = append(slices.Clone(during), "COHERE_TEST_WRITE_AFTER_GO_ENGINE="+path,
				"COHERE_TEST_WRITE_AFTER_GO_ENGINE_TEXT="+change.text)
			if changed := fixture.run(true, arguments...); isGoReplay(changed) {
				test.Fatalf("the run with the change in it replayed, so it proves nothing:\n%s", changed.stdout)
			}
			fixture.environment = during
			if contents, readError := os.ReadFile(path); readError != nil || string(contents) != change.text {
				test.Fatalf("the instrument did not write %s, so nothing below is about it: %v", path, readError)
			}

			after := fixture.run(true, arguments...)
			cold := fixture.run(false, arguments...)
			if isGoReplay(after) {
				test.Fatalf("a run recorded over a change made while it ran was replayed:\n%s", after.stdout)
			}
			if goNormalized(after.stdout) != goNormalized(cold.stdout) || after.exitCode != cold.exitCode {
				test.Fatalf("the run after the change differs from a cold run:\n--- after\n%s\n--- cold\n%s",
					after.stdout, cold.stdout)
			}
		})
	}
}

// TestAGoRunThatWritesIsNeverRecorded: a bare run whose fix phase formats a file is never recorded, since its record
// would be signed by the tree it wrote and replay "1 of N files rewritten" over a tree nothing touched. And a
// `--no-fix` run's record never answers a bare run, which writes where `--no-fix` only reports.
func TestAGoRunThatWritesIsNeverRecorded(test *testing.T) {
	test.Parallel()
	fixture := newGoReplayFixture(test)
	fixture.write("spaced.go", "package replay\n\n// Spaced is spaced.\nfunc  Spaced() int { return 1 }\n")
	fixture.establishHit("--no-fix", "--verbose")

	writing := fixture.run(true, "--verbose")
	if isGoReplay(writing) {
		test.Fatalf("a bare run replayed the --no-fix run's record:\n%s", writing.stdout)
	}
	rewrote := regexp.MustCompile(`(?m)^fix: 1 of \d+ files rewritten`)
	if !rewrote.MatchString(writing.stdout) {
		test.Fatalf("the bare run rewrote nothing, so this case proves nothing:\n%s", writing.stdout)
	}
	next := fixture.run(true, "--verbose")
	cold := fixture.run(false, "--verbose")
	if isGoReplay(next) || rewrote.MatchString(next.stdout) {
		test.Fatalf("the run that rewrote a file was replayed over a tree nothing touched:\n%s", next.stdout)
	}
	if goNormalized(next.stdout) != goNormalized(cold.stdout) || next.exitCode != cold.exitCode {
		test.Fatalf("the run after the rewrite differs from a cold run:\n--- next\n%s\n--- cold\n%s",
			next.stdout, cold.stdout)
	}
}
