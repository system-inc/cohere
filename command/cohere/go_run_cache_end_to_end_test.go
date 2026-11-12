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

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/executable_testing"
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

// goReplaySettings is the replay fixtures' settings: no Go rule changed, and the settings file itself left out of
// the format walk, the one file of theirs the house formatter reads, so a root of them is the Go module alone. A
// format project beside it (#4wjhppw) would make every run two projects, whose replays are each their own, and these
// tests are about the Go engine's.
const goReplaySettings = `{"go":{"rules":{}},"ignorePatterns":["CohereSettings.json"]}`

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
	fixture.write("CohereSettings.json", goReplaySettings)
	fixture.write("a.go", goReplaySource("replay", "Greet"))
	fixture.write("sub/b.go", "package sub\n\n// Answer answers.\nfunc Answer() int {\n\treturn 42\n}\n")
	// A directory `./...` reaches that holds no Go file, so only its listing says when a package appears below it.
	fixture.write("notes/README.txt", "notes\n")
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

// variable is the value every run from here on gets for name, empty when the fixture sets none.
func (fixture *goReplayFixture) variable(name string) string {
	for _, variable := range slices.Backward(fixture.environment) {
		if value, found := strings.CutPrefix(variable, name+"="); found {
			return value
		}
	}
	return ""
}

// goStandInRoot is a `GOROOT` of the test's own: every entry of the real one linked, but `VERSION` and go.env, which
// are copies a scenario may rewrite without touching the toolchain every other test runs.
func goStandInRoot(test *testing.T) string {
	test.Helper()
	output, outputError := exec.Command("go", "env", "GOROOT").Output()
	if outputError != nil {
		test.Fatalf("reading GOROOT: %v", outputError)
	}
	realRoot := strings.TrimSpace(string(output))
	entries, readError := os.ReadDir(realRoot)
	if readError != nil {
		test.Fatal(readError)
	}
	standIn := filepath.Join(test.TempDir(), "goroot")
	if createDirectoryError := os.Mkdir(standIn, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	for _, entry := range entries {
		source, target := filepath.Join(realRoot, entry.Name()), filepath.Join(standIn, entry.Name())
		if entry.Name() != "VERSION" && entry.Name() != "go.env" {
			if linkError := os.Symlink(source, target); linkError != nil {
				test.Fatal(linkError)
			}
			continue
		}
		contents, readFileError := os.ReadFile(source)
		if readFileError != nil {
			test.Fatal(readFileError)
		}
		if writeError := os.WriteFile(target, contents, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return standIn
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
	commandLine := slices.Clone(arguments)
	if !cached {
		commandLine = append(commandLine, "--no-cache")
	}
	command := exec.Command(fixture.binary, commandLine...)
	command.Dir = fixture.root
	command.Env = append(withoutVariable(os.Environ(), goEngineChildVariable), fixture.environment...)
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

// goReplayFooter is the footer a `--verbose` replay renders of its own, which says it was replayed.
var goReplayFooter = regexp.MustCompile(`(?m)^(✓ 💎|✗ ☠️) [0-9.]+s(?: •[^\n]*)? • replayed`)

// goEveryFileCachedFooter is the default view's footer when no file was checked and every one was cached, followed
// by whatever the run did not check, such as the fixture's files only another platform or cgo builds.
var goEveryFileCachedFooter = regexp.MustCompile(
	`(?m)^(✓ 💎|✗ ☠️) [0-9.]+s •.*\(\d+ rules • \d+ cached\)(?: • ⚠ [^\n]*)?$`,
)

// isGoReplay reports a run the cache answered, by what the run itself says: the framing on its first line and the
// footer saying so under `--verbose`, the summary's `cache.replayed` under `--json`, and in the default view a
// footer that checked no file and found every one cached, which only a replay prints for a Go run. A replay never
// starts the engine, so this is also the proof that none was started: a run of the engine checks its files.
func isGoReplay(run goReplayRun) bool {
	return strings.HasPrefix(run.stdout, "cached: ") || goReplayFooter.MatchString(run.stdout) ||
		strings.Contains(run.stdout, `"replayed":true`) || goEveryFileCachedFooter.MatchString(run.stdout)
}

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
		"  lint: package cache ",
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
func goSummaryVerdict(test *testing.T, output string) map[string]json.RawMessage {
	test.Helper()
	var summary map[string]json.RawMessage
	if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, output)), &summary); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, output)
	}
	for _, invocation := range []string{
		"seconds", "graphSeconds", "formattingSeconds", "phases", "cache", "packageCache", "checked", "cached", "nodes",
		"memoryRaisedToBytes",
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
				// A replay started no engine, so it says nothing of the engine's package cache, nor of a memory raise
				// (#5shepvt).
				for _, invocationOnly := range []string{`"packageCache"`, `"memoryRaisedToBytes"`} {
					if strings.Contains(summaryJsonOf(test, replayed.stdout), invocationOnly) {
						test.Errorf("the replay's summary claims the engine's %s:\n%s", invocationOnly, replayed.stdout)
					}
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

// jsonText is a summary as JSON again, its keys sorted, so two decoded summaries compare as text. Each value is the
// bytes one encoder wrote, so equal values are equal bytes.
func jsonText(test *testing.T, summary map[string]json.RawMessage) string {
	test.Helper()
	encoded, encodeError := json.Marshal(summary)
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
		name string
		// prepare, when there is one, shapes the fixture before its hit is established.
		prepare func(fixture *goReplayFixture)
		change  func(fixture *goReplayFixture)
	}{
		// The same size, so only the bytes and the modification time moved, as an editor's save of one character.
		{"a .go file's bytes edited in place", nil, func(fixture *goReplayFixture) {
			// The new modification time must differ from the recorded one at the file system's resolution.
			time.Sleep(10 * time.Millisecond)
			fixture.write("a.go", strings.Replace(goReplaySource("replay", "Greet"), `"%d"`, `"%s"`, 1))
		}},
		// The same size and the old modification time, as cp -p, rsync -t and touch -r leave a file: only the change
		// time and the bytes moved.
		{"a same-size edit with its modification time restored", nil, func(fixture *goReplayFixture) {
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
		{"an embedded file edited", nil, func(fixture *goReplayFixture) {
			fixture.write("data/message.txt", "hello, again\n")
		}},
		// Its constraint taken off, so a file the load left out joins it.
		{"a build-tagged file edited", nil, func(fixture *goReplayFixture) {
			fixture.write("tagged.go", goReplaySource("replay", "Tagged"))
		}},
		{"a .go file added to a package directory", nil, func(fixture *goReplayFixture) {
			fixture.write("sub/c.go", goReplaySource("sub", "Added"))
		}},
		// Below a directory that holds no Go file, so no file the load read sits beside it, and only the listed
		// directory's signature moves.
		{"a package directory added", nil, func(fixture *goReplayFixture) {
			fixture.write("notes/fresh/f.go", goReplaySource("fresh", "Fresh"))
		}},
		{"go.mod edited", nil, func(fixture *goReplayFixture) {
			fixture.write("go.mod", "module example.com/replay\n\ngo 1.27\n\n// edited\n")
		}},
		{"go.sum edited", nil, func(fixture *goReplayFixture) {
			fixture.write("go.sum", "example.com/other v1.0.0/go.mod h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=\n")
		}},
		// Held by the front door's own root resolution rather than by the engine's absent paths: a module root loads
		// with `GOWORK=off`, and a go.work that uses the module moves the root, and with it the key and the cache
		// directory, before anything is checked. The go command's settings file below is the absent-path case.
		{"a go.work created in an ancestor directory", nil, func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.parent, "go.work")
			if writeError := os.WriteFile(path, []byte("go 1.27\n\nuse ./module\n"), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
			// The workspace is the root now, so its settings are the ones that keep every settings file, its own and
			// the module's, out of the format walk, as goReplaySettings does at the module.
			settings := filepath.Join(fixture.parent, "CohereSettings.json")
			contents := []byte(`{"ignorePatterns":["**/CohereSettings.json"]}`)
			if writeError := os.WriteFile(settings, contents, 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		// The absent-path case: the variable naming the file is the same before and after, so only the file's absence,
		// recorded, can tell.
		{"the go command's settings file created", nil, func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.parent, "go.env")
			if writeError := os.WriteFile(path, []byte("GOFLAGS=-tags=replaytest\n"), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		// staticcheck's config analyzer reads a staticcheck.conf in every directory above a package (ST1001 and
		// ST1013 read it), so one appearing above the module changes what the rules see, and only its recorded
		// absence can tell.
		{"a staticcheck.conf created above the module", nil, func(fixture *goReplayFixture) {
			path := filepath.Join(fixture.parent, "staticcheck.conf")
			if writeError := os.WriteFile(path, []byte("checks = [\"inherit\"]\n"), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		{"the settings file edited", nil, func(fixture *goReplayFixture) {
			fixture.write("CohereSettings.json", strings.Replace(goReplaySettings, `{"rules":{}}`,
				`{"rules":{"cohere-go/printf":"off"}}`, 1))
		}},
		{"GOFLAGS changed", nil, func(fixture *goReplayFixture) {
			fixture.setVariable("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -tags=replaytest"))
		}},
		// A target other than the host, so only_linux.go joins the load or leaves it.
		{"GOOS changed", nil, func(fixture *goReplayFixture) {
			target := "linux"
			if goHostIsLinux() {
				target = "darwin"
			}
			fixture.setVariable("GOOS", target)
		}},
		{"CGO_ENABLED changed", nil, func(fixture *goReplayFixture) { fixture.setVariable("CGO_ENABLED", "1") }},
		// The toolchain that ran, as the engine names it: a `GOROOT` of the fixture's own, its tree the real one's by
		// links and its `VERSION` and go.env copies, so the `VERSION` a toolchain's update rewrites can be rewritten
		// here.
		// `GOROOT` is set before the hit and never changes, so only the recorded file can tell.
		{"the toolchain's VERSION rewritten", func(fixture *goReplayFixture) {
			fixture.setVariable("GOROOT", goStandInRoot(fixture.test))
		}, func(fixture *goReplayFixture) {
			version := filepath.Join(fixture.variable("GOROOT"), "VERSION")
			contents, readError := os.ReadFile(version)
			if readError != nil {
				fixture.test.Fatal(readError)
			}
			// Its first line is the toolchain's version, which the go command holds itself to, so the stamp below it
			// is what moves, as a rebuild of the same version moves it.
			time.Sleep(10 * time.Millisecond)
			if writeError := os.WriteFile(version, append(contents, "rebuilt\n"...), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}},
		// A directory of its own holding a go that runs the real one, first on PATH, so the cold run still loads.
		{"the go on PATH changed", nil, func(fixture *goReplayFixture) {
			realGo, lookPathError := exec.LookPath("go")
			if lookPathError != nil {
				fixture.test.Fatal(lookPathError)
			}
			toolchain := fixture.test.TempDir()
			script := "#!/bin/sh\nexec '" + realGo + "' \"$@\"\n"
			if writeError := executable_testing.WriteFile(
				filepath.Join(toolchain, "go"),
				[]byte(script),
				0o755,
			); writeError != nil {
				fixture.test.Fatal(writeError)
			}
			fixture.setVariable("PATH", toolchain+string(os.PathListSeparator)+os.Getenv("PATH"))
		}},
	}
	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			if scenario.prepare != nil {
				scenario.prepare(fixture)
			}
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

// goHostIsLinux reports whether the tests run on Linux, so the `GOOS` scenario names a target other than the host.
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
			// The flag reached the engine, whose own per-package cache it turns off too.
			if !strings.Contains(cold.stdout, "\n  lint: package cache off\n") {
				test.Errorf("the engine's package cache was not off:\n%s", cold.stdout)
			}
		case "--json":
			var summary struct {
				Cache struct {
					Off      bool `json:"off"`
					Replayed bool `json:"replayed"`
				} `json:"cache"`
				PackageCache *packageCacheUse `json:"packageCache"`
			}
			if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, cold.stdout)), &summary); decodeError != nil {
				test.Fatal(decodeError)
			}
			if !summary.Cache.Off || summary.Cache.Replayed {
				test.Errorf("the summary's cache reads off %t, replayed %t:\n%s", summary.Cache.Off,
					summary.Cache.Replayed, cold.stdout)
			}
			if summary.PackageCache == nil || summary.PackageCache.On {
				test.Errorf("the summary does not say the engine's package cache was off:\n%s", cold.stdout)
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
	entries, readError := os.ReadDir(cache_directory.Of(root))
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
				staticcheckConfiguration := filepath.Join(fixture.parent, "staticcheck.conf")
				contents := []byte("checks = [\"inherit\"]\n")
				if writeError := os.WriteFile(staticcheckConfiguration, contents, 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			arguments := []string{"--no-fix", "--verbose"}
			fixture.establishHit(arguments...)
			// With the engine's own per-package cache on: the recording run wrote its entries under the root's
			// .cache/cohere/go, during the run and after the clock, and none of them kept the run from replaying.
			if entries := goEngineCacheEntries(test, fixture.root); entries == 0 {
				test.Errorf("the engine's package cache wrote no entry, so it was not on for these runs")
			}
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

// goEngineCacheEntries counts the files the Go engine's per-package findings cache holds under root.
func goEngineCacheEntries(test *testing.T, root string) int {
	test.Helper()
	entries := 0
	walkError := filepath.WalkDir(filepath.Join(cache_directory.Of(root), "go"),
		func(path string, entry os.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if !entry.IsDir() {
				entries++
			}
			return nil
		})
	if walkError != nil && !errors.Is(walkError, os.ErrNotExist) {
		test.Fatal(walkError)
	}
	return entries
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

// TestEveryProjectOfAMixedRootReplays: at a root holding a Go module and a TypeScript project, each checked by a
// child of the run (`cohere --directory <project> ...`, see runProject), the second run replays both children, the
// Go one without starting its engine. Before discovery's children were admitted every one of them ran cold, cohere's
// own root among them, since `--directory` admits no run.
func TestEveryProjectOfAMixedRootReplays(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		".gitignore": ".cache/\nnode_modules/\n",
		// Only the repository's root ignores .cache/, as a real repository does, so no project's section opens on a
		// note that its cache would be tracked.
		"service/go.mod": "module example.com/service\n\ngo 1.27\n",
		"service/go.sum": "",
		"service/a.go":   goReplaySource("service", "Greet"),
		"web/tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext",` +
			`"moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},` +
			`"include":["source"]}`,
		"web/CohereSettings.json": `{"rules":{"nexus/consistency-no-ambiguous-identifier":"off",` +
			`"@typescript-eslint/no-inferrable-types":"off","no-debugger":"error"}}`,
		"web/package.json":            `{"name":"fixture","private":true,"type":"module"}`,
		"web/source/a.ts":             "export const a: number = 1;\n",
		"service/CohereSettings.json": `{"go":{"rules":{}}}`,
	})
	run := func() string {
		test.Helper()
		command := exec.Command(binary, "--no-fix", "--no-format", "--verbose")
		command.Dir = root
		command.Env = append(withoutVariable(os.Environ(), goEngineChildVariable), "COHERE_VERDICT_FD=")
		output, runError := command.CombinedOutput()
		var exitError *exec.ExitError
		if runError != nil && !errors.As(runError, &exitError) {
			test.Fatalf("running cohere: %v\n%s", runError, output)
		}
		return string(output)
	}
	// Each project's section, by the engine its heading names, from the merged report.
	sections := func(output string) map[string]string {
		found := map[string]string{}
		heading := regexp.MustCompile(`(?m)^== (\S+) \((Go|TypeScript)\) ==\n`)
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

	first := run()
	if len(sections(first)) != 2 {
		test.Fatalf("the root was not checked as two projects:\n%s", first)
	}
	second := sections(run())
	for _, engine := range []string{"Go", "TypeScript"} {
		section, found := second[engine]
		if !found {
			test.Fatalf("no %s section in the second run:\n%v", engine, second)
		}
		if !strings.HasPrefix(section, "cached: ") {
			test.Errorf("the %s project ran again rather than replaying:\n%s", engine, section)
		}
	}
	if regexp.MustCompile(`(?m)^module described in `).MatchString(second["Go"]) {
		test.Errorf("the Go project's engine ran on the second run:\n%s", second["Go"])
	}
}

// TestAGoModuleAndItsFormatProjectEachReplay: at a Go module's root holding Markdown, discovery runs the module and
// the format project beside it, two children given the same arguments in one directory (#4wjhppw). Each records under
// its own engine's name in the root's cache table, so the second run replays both. Under one name each run would
// replay neither, whichever recorded last having overwritten the other.
func TestAGoModuleAndItsFormatProjectEachReplay(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		".gitignore":          ".cache/\n",
		"go.mod":              "module example.com/formatted\n\ngo 1.27\n",
		"go.sum":              "",
		"a.go":                goReplaySource("formatted", "Greet"),
		"CohereSettings.json": "{ \"go\": { \"rules\": {} } }\n",
		"README.md":           "# formatted\n",
	})
	run := func() map[string]string {
		test.Helper()
		command := exec.Command(binary, "--no-fix", "--verbose")
		command.Dir = root
		command.Env = append(
			withoutVariable(os.Environ(), goEngineChildVariable),
			"COHERE_VERDICT_FD=",
			"CGO_ENABLED=0",
		)
		output, runError := command.CombinedOutput()
		var exitError *exec.ExitError
		if runError != nil && !errors.As(runError, &exitError) {
			test.Fatalf("running cohere: %v\n%s", runError, output)
		}
		found := map[string]string{}
		heading := regexp.MustCompile(`(?m)^== (\S+) \((Go|Format)\) ==\n`)
		bounds := heading.FindAllStringSubmatchIndex(string(output), -1)
		for index, bound := range bounds {
			end := len(output)
			if index+1 < len(bounds) {
				end = bounds[index+1][0]
			}
			found[string(output[bound[4]:bound[5]])] = string(output[bound[1]:end])
		}
		return found
	}

	if first := run(); len(first) != 2 {
		test.Fatalf("the root was not checked as the module and its format project:\n%v", first)
	}
	second := run()
	for _, engine := range []string{"Go", "Format"} {
		if section := second[engine]; !strings.HasPrefix(section, "cached: ") {
			test.Errorf("the %s project ran again rather than replaying:\n%s", engine, section)
		}
	}
}

// TestAGoWorkspaceInAWorktreeReplaysOverItsGitDirectory: a go.work root checked out as a git worktree, whose record
// names the index in the main repository's git directory, the file its gitlinks are read from. A file created
// beside that index, as every git command in the worktree creates a lock file there, leaves the next run a replay,
// since only the index decides the verdict and the directory holding it is outside the roots the run walked. The index
// is keyed on what it decides (#m1daxxk): a mode staged for a source file replays, and a gitlink appearing misses.
func TestAGoWorkspaceInAWorktreeReplaysOverItsGitDirectory(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	base := test.TempDir()
	main, worktree := filepath.Join(base, "main"), filepath.Join(base, "worktree")
	git := func(directory string, arguments ...string) {
		test.Helper()
		command := exec.Command("git", append([]string{"-c", "user.email=fixture@example.com", "-c",
			"user.name=fixture"}, arguments...)...)
		command.Dir = directory
		if output, gitError := command.CombinedOutput(); gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
	}
	writeTree(test, main, map[string]string{
		".gitignore":          ".cache/\n",
		"go.work":             "go 1.27\n\nuse ./module\n",
		"module/go.mod":       "module example.com/worked\n\ngo 1.27\n",
		"module/go.sum":       "",
		"module/a.go":         goReplaySource("worked", "Greet"),
		"CohereSettings.json": goReplaySettings,
	})
	git(main, "init", "--quiet")
	git(main, "add", "-A")
	git(main, "commit", "--quiet", "-m", "fixture")
	git(main, "worktree", "add", "--quiet", worktree)
	gitDirectory := filepath.Join(main, ".git", "worktrees", "worktree")
	if _, statError := os.Stat(filepath.Join(gitDirectory, "index")); statError != nil {
		test.Fatalf("the worktree has no index in the main repository's git directory: %v", statError)
	}
	fixture := &goReplayFixture{test: test, binary: binary, parent: base, root: worktree,
		environment: []string{"CGO_ENABLED=0"}}
	arguments := []string{"--no-fix", "--verbose"}
	fixture.establishHit(arguments...)

	sibling := filepath.Join(gitDirectory, "sibling.lock")
	if writeError := os.WriteFile(sibling, []byte("held\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if replayed := fixture.run(true, arguments...); !isGoReplay(replayed) {
		test.Fatalf("a file created beside the worktree's index kept the next run from replaying:\n%s%s",
			replayed.stdout, replayed.stderr)
	}

	// The index's bytes alone: a mode staged for a file no ignore rule excludes decides nothing for the run, whose
	// verdict reads that file's bytes, so the index keyed on what it decides still replays (#m1daxxk).
	git(worktree, "update-index", "--chmod=+x", filepath.Join("module", "a.go"))
	if replayed := fixture.run(true, arguments...); !isGoReplay(replayed) {
		test.Fatalf("a mode staged in the worktree's index, which decides nothing, kept the run from replaying:\n%s%s",
			replayed.stdout, replayed.stderr)
	}

	// What the index decides: a gitlink, which a go.work root reads its submodules from, misses.
	head, headError := exec.Command("git", "-C", worktree, "rev-parse", "HEAD").Output()
	if headError != nil {
		test.Fatal(headError)
	}
	git(worktree, "update-index", "--add", "--cacheinfo",
		"160000,"+strings.TrimSpace(string(head))+","+filepath.ToSlash(filepath.Join("module", "linked")))
	if changed := fixture.run(true, arguments...); isGoReplay(changed) {
		test.Fatalf("a gitlink appeared in the worktree's index and the run replayed:\n%s", changed.stdout)
	}
}

// TestAGoRunTheRecordCannotNameIsNeverRecorded: a run that reads what no inputs record can name is never recorded,
// and says so in the account. A project with cgo of its own is loaded through the cgo tool, which reads system C
// headers and a compiler; a `GOFLAGS` carrying `-overlay` replaces source files by others nothing names. In each, the
// second run of an unchanged module is a miss, and the reason is said on both.
func TestAGoRunTheRecordCannotNameIsNeverRecorded(test *testing.T) {
	test.Parallel()
	for _, shape := range []struct {
		name    string
		prepare func(fixture *goReplayFixture)
		reason  string
	}{
		{"a project with cgo of its own", func(fixture *goReplayFixture) {
			fixture.setVariable("CGO_ENABLED", "1")
			fixture.write("native.go", "package replay\n\n// #include <stdlib.h>\nimport \"C\"\n\n"+
				"// Absolute is the magnitude of value, from C's abs.\nfunc Absolute(value int) int {\n"+
				"\treturn int(C.abs(C.int(value)))\n}\n")
		}, "the run cache did not record this run: the load translated cgo"},
		{"an overlay in GOFLAGS", func(fixture *goReplayFixture) {
			overlay := filepath.Join(fixture.parent, "overlay.json")
			if writeError := os.WriteFile(overlay, []byte(`{"Replace":{}}`), 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
			fixture.setVariable("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -overlay="+overlay))
		}, "the run cache does not record this run: GOFLAGS carries -overlay"},
	} {
		test.Run(shape.name, func(test *testing.T) {
			test.Parallel()
			fixture := newGoReplayFixture(test)
			shape.prepare(fixture)
			arguments := []string{"--no-fix", "--verbose"}
			for _, attempt := range []string{"the first run", "the second run"} {
				run := fixture.run(true, arguments...)
				if isGoReplay(run) {
					test.Fatalf("%s replayed:\n%s", attempt, run.stdout)
				}
				if !strings.Contains(run.stdout+run.stderr, shape.reason) {
					test.Errorf("%s did not say why it was not recorded, %q:\n%s%s", attempt, shape.reason,
						run.stdout, run.stderr)
				}
			}
		})
	}
}
