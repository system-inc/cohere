package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMain turns the run cache off for every binary this package's tests launch.
//
// A bare `cohere` became eligible for the run cache, and child processes inherit the test process's
// environment. Without this, every test here that runs the binary bare would record into the
// developer's real user cache, and one that ran it twice over an unchanged fixture would get a replay
// instead of the run it exists to test, while still passing. Off here keeps each of those tests
// meaning exactly what it meant before the cache existed. TestRunCacheEndToEnd turns it back on for
// its own runs, against an isolated home.
func TestMain(m *testing.M) {
	os.Setenv("COHERE_RUN_CACHE", "off")
	os.Exit(m.Run())
}

// TestRunCacheEndToEnd is the run cache's proof against the real binary over a real git repository.
//
// For each input category: establish a hit, make the change, and require that the next run does not
// replay, reports exactly what a cold run reports, and that the run after it hits and replays the new
// truth. The package tests prove the parts; this proves the assembled command, where the bugs were:
// three of them reached a real tree before anything here existed.
//
// Each scenario has been shown failing against a binary broken in the way it guards: the scope fact
// ignored, the input recorder off, the fix-run decline removed, the invocation lines never tagged.
func TestRunCacheEndToEnd(t *testing.T) {
	binary := buildCohere(t)
	home := t.TempDir()
	root := t.TempDir()

	git := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = root
		command.Env = append(os.Environ(), "HOME="+home)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	remove := func(name string) {
		t.Helper()
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) {
		t.Helper()
		git("add", "-A")
		git("commit", "--quiet", "--allow-empty", "-m", message)
	}

	write("tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`)
	write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error"}}`)
	write("package.json", `{"name":"fixture","private":true,"type":"module"}`)
	write(".gitignore", ".cache/\nnode_modules/\n")
	write("source/a.ts", "export const a: number = 1;\n")
	write("source/nested/b.ts", "import { a } from \"../a\";\nexport const b = a + 1;\n")
	write("docs/keep.md", "keep\n")
	git("init", "--quiet")
	git("config", "user.email", "fixture@example.com")
	git("config", "user.name", "fixture")
	commit("initial")

	// run launches the binary bare, which is the only shape the run cache serves. cached false is the
	// cold truth every cached result is held against.
	run := func(cached bool) (string, int) {
		t.Helper()
		command := exec.Command(binary)
		command.Dir = root
		environment := []string{"HOME=" + home}
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, "COHERE_RUN_CACHE=") && !strings.HasPrefix(variable, "HOME=") {
				environment = append(environment, variable)
			}
		}
		if !cached {
			environment = append(environment, "COHERE_RUN_CACHE=off")
		}
		command.Env = environment
		output, err := command.CombinedOutput()
		if err == nil {
			return string(output), 0
		}
		exitError, isExit := err.(*exec.ExitError)
		if !isExit {
			t.Fatalf("running cohere: %v\n%s", err, output)
		}
		return string(output), exitError.ExitCode()
	}

	durations := regexp.MustCompile(`\d+(\.\d+)?(ms|s|µs)\b`)
	normalized := func(output string) string { return durations.ReplaceAllString(output, "T") }
	isReplay := func(output string) bool { return strings.HasPrefix(output, "cached: ") }
	keepLines := func(output string, drop ...string) string {
		kept := []string{}
	line:
		for _, line := range strings.Split(output, "\n") {
			for _, prefix := range drop {
				if strings.HasPrefix(line, prefix) {
					continue line
				}
			}
			kept = append(kept, line)
		}
		return strings.Join(kept, "\n")
	}
	// A replay must equal the cold run's verdict: its lines minus the ones that describe that
	// invocation, which a replay deliberately does not print. Its own framing comes off first.
	verdict := func(cold string) string {
		return keepLines(cold, "graph built in ", "types: ", "lint: ", "phases: ", "  total ")
	}
	// A replay says which run a phase's line came from; that label comes off before the comparison,
	// and is required separately below so a replay that lost it fails.
	provenance := regexp.MustCompile(`^fix \(from the cached run at \d\d:\d\d:\d\d\): `)
	replayBody := func(replay string) string {
		lines := strings.Split(keepLines(replay, "cached: ", "phases: replayed ", "  this run: "), "\n")
		for index, line := range lines {
			lines[index] = provenance.ReplaceAllString(line, "fix: ")
		}
		return strings.Join(lines, "\n")
	}

	establishHit := func() {
		t.Helper()
		run(true)
		if output, _ := run(true); !isReplay(output) {
			t.Fatalf("an unchanged tree did not replay, so nothing below can show a change was noticed:\n%s", output)
		}
	}

	scenarios := []struct {
		name    string
		prepare func()
		change  func()
		undo    func()
	}{
		// Type errors rather than lint findings: a finding with a fixer is rewritten by the run that
		// sees it, which is the fix-run case below and a different property.
		{"a source file edited to add a finding", nil,
			func() { write("source/a.ts", "export const a: number = \"x\";\n") },
			func() { write("source/a.ts", "export const a: number = 1;\n") }},
		{"a file added at the top level", nil,
			func() { write("source/z.ts", "export const z: number = \"x\";\n") },
			func() { remove("source/z.ts") }},
		// The case the design first missed: a file added one directory down moves only that
		// directory's mtime.
		{"a file added in a nested directory", nil,
			func() { write("source/nested/y.ts", "export const y: number = \"x\";\n") },
			func() { remove("source/nested/y.ts") }},
		{"a file deleted", nil,
			func() { remove("source/nested/b.ts") },
			func() { write("source/nested/b.ts", "import { a } from \"../a\";\nexport const b = a + 1;\n") }},
		{"the lint config changed", nil,
			func() {
				write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error","prefer-const":"error"}}`)
			},
			func() { write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error"}}`) }},
		{"the tsconfig changed", nil,
			func() {
				write("tsconfig.json", `{"compilerOptions":{"strict":false,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`)
			},
			func() {
				write("tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`)
			}},
		{"package.json edited in place", nil,
			func() { write("package.json", `{"name":"fixture","private":true,"type":"commonjs"}`) },
			func() { write("package.json", `{"name":"fixture","private":true,"type":"module"}`) }},
		// node_modules is ignored, so git never mentions it and the scope fact cannot see this. Only
		// the input recorder can: the build read this declaration, so its signature is an input.
		{"a dependency's declaration changed",
			func() {
				write("node_modules/dep/package.json", `{"name":"dep","types":"index.d.ts"}`)
				write("node_modules/dep/index.d.ts", "export declare const d: number;\n")
				write("source/u.ts", "import { d } from \"dep\";\nexport const u: number = d;\n")
			},
			func() { write("node_modules/dep/index.d.ts", "export declare const d: string;\n") },
			func() { remove("source/u.ts"); remove("node_modules") }},
		// A commit makes a changed file unchanged and moves no file's mtime: only the scope fact sees it.
		{"an uncommitted change committed",
			func() { write("source/a.ts", "export const a: number = 2;\n") },
			func() { commit("commit the change") },
			func() { write("source/a.ts", "export const a: number = 1;\n"); commit("restore") }},
		// docs exists and is tracked, and the build reads nothing in it, so no watched directory moves:
		// only the scope fact sees a file added here. Created as a new directory instead, it would move
		// the project root, which is watched, and the case would pass with the fact switched off.
		{"an untracked file in a directory the build never reads", nil,
			func() { write("docs/notes.md", "notes\n") },
			func() { remove("docs/notes.md") }},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.prepare != nil {
				scenario.prepare()
			}
			establishHit()
			scenario.change()
			defer scenario.undo()

			afterChange, afterChangeExit := run(true)
			cold, coldExit := run(false)
			if isReplay(afterChange) {
				t.Fatalf("the change was not noticed: the recorded run was replayed as current:\n%s", afterChange)
			}
			if normalized(afterChange) != normalized(cold) || afterChangeExit != coldExit {
				t.Fatalf("the run after the change differs from a cold run (exit %d against %d):\n--- after the change\n%s\n--- cold\n%s",
					afterChangeExit, coldExit, afterChange, cold)
			}

			next, nextExit := run(true)
			if !isReplay(next) {
				t.Fatalf("the run after that did not replay the new truth:\n%s", next)
			}
			if !provenance.MatchString(keepLines(next, "cached: ")) {
				t.Fatalf("the replay printed the fix line as though the fix phase had just run:\n%s", next)
			}
			if replayBody(next) != verdict(cold) || nextExit != coldExit {
				t.Fatalf("the replay is not the cold run's verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
					nextExit, coldExit, replayBody(next), verdict(cold))
			}
		})
	}

	// A run whose fix phase rewrites a file is never replayed. The record step stats inputs after the
	// rewrite, so without the decline the next run would match the fixed tree and replay "1 of 1 files
	// rewritten" over a tree it never touched.
	t.Run("a fix run is not replayed", func(t *testing.T) {
		establishHit()
		write("source/a.ts", "export const a: number = 1;\ndebugger;\n")
		defer write("source/a.ts", "export const a: number = 1;\n")

		fixRun, _ := run(true)
		if !strings.Contains(fixRun, "1 of 1 files rewritten") {
			t.Fatalf("the fix run rewrote nothing, so this case proves nothing:\n%s", fixRun)
		}
		next, _ := run(true)
		cold, _ := run(false)
		if strings.Contains(next, "files rewritten, 1 fixes applied") {
			t.Fatalf("the rewrite was replayed over a tree nothing touched:\n%s", next)
		}
		if normalized(next) != normalized(cold) {
			t.Fatalf("the run after the fix differs from a cold run:\n--- next\n%s\n--- cold\n%s", next, cold)
		}
	})
}
