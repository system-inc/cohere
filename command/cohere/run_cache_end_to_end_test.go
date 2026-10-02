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

	// run launches the binary bare unless arguments are given; bare and `--no-fix` are the shapes the
	// run cache serves. cached false is the cold truth every cached result is held against.
	run := func(cached bool, arguments ...string) (string, int) {
		t.Helper()
		command := exec.Command(binary, arguments...)
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
	// The findings cache's clause says how much of the verdict was remembered; a cold run never has it,
	// so it comes off before a comparison and is required or forbidden separately per scenario.
	layerTwoClause := regexp.MustCompile(`; \d+ of \d+ files replayed from cache`)
	normalized := func(output string) string {
		return layerTwoClause.ReplaceAllString(durations.ReplaceAllString(output, "T"), "")
	}
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

	// layerTwo says whether the run after the change should serve unchanged files from the findings
	// cache. A change to the lint config or the tsconfig changes the cache's key, so nothing may be
	// replayed; any other change leaves the untouched files replayable.
	scenarios := []struct {
		name     string
		layerTwo bool
		prepare  func()
		change   func()
		undo     func()
	}{
		// Type errors rather than lint findings: a finding with a fixer is rewritten by the run that
		// sees it, which is the fix-run case below and a different property.
		{"a source file edited to add a finding", true, nil,
			func() { write("source/a.ts", "export const a: number = \"x\";\n") },
			func() { write("source/a.ts", "export const a: number = 1;\n") }},
		{"a file added at the top level", true, nil,
			func() { write("source/z.ts", "export const z: number = \"x\";\n") },
			func() { remove("source/z.ts") }},
		// The case the design first missed: a file added one directory down moves only that
		// directory's mtime.
		{"a file added in a nested directory", true, nil,
			func() { write("source/nested/y.ts", "export const y: number = \"x\";\n") },
			func() { remove("source/nested/y.ts") }},
		{"a file deleted", true, nil,
			func() { remove("source/nested/b.ts") },
			func() { write("source/nested/b.ts", "import { a } from \"../a\";\nexport const b = a + 1;\n") }},
		{"the lint config changed", false, nil,
			func() {
				write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error","prefer-const":"error"}}`)
			},
			func() { write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error"}}`) }},
		// Only the base is edited, and it is untracked, so git reports the same changed files before and
		// after and the scope fact cannot see it. The run cache's declared inputs and the findings
		// cache's key must each name every file in the extends chain, or both replay the old verdict.
		{"a base the lint config extends changed", false,
			func() {
				write("lint/base.json", `{"rules":{"no-debugger":"error","no-var":"error"}}`)
				write("CohereSettings.json", `{"extends":"./lint/base.json","rules":{}}`)
			},
			func() {
				write("lint/base.json", `{"rules":{"no-debugger":"error","no-var":"error","prefer-const":"error"}}`)
			},
			func() {
				write("CohereSettings.json", `{"rules":{"no-debugger":"error","no-var":"error"}}`)
				remove("lint")
			}},
		{"the tsconfig changed", false, nil,
			func() {
				write("tsconfig.json", `{"compilerOptions":{"strict":false,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`)
			},
			func() {
				write("tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`)
			}},
		{"package.json edited in place", true, nil,
			func() { write("package.json", `{"name":"fixture","private":true,"type":"commonjs"}`) },
			func() { write("package.json", `{"name":"fixture","private":true,"type":"module"}`) }},
		// node_modules is ignored, so git never mentions it and the scope fact cannot see this. Only
		// the input recorder can: the build read this declaration, so its signature is an input.
		{"a dependency's declaration changed", true,
			func() {
				write("node_modules/dep/package.json", `{"name":"dep","types":"index.d.ts"}`)
				write("node_modules/dep/index.d.ts", "export declare const d: number;\n")
				write("source/u.ts", "import { d } from \"dep\";\nexport const u: number = d;\n")
			},
			func() { write("node_modules/dep/index.d.ts", "export declare const d: string;\n") },
			func() { remove("source/u.ts"); remove("node_modules") }},
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
			// The clause rides on the lint line, and a run whose types bail prints no lint line at all, as a
			// cold run does not either: the walk still replayed, but no lint verdict is reported to be
			// remembered. So it is required only where a lint line was printed.
			replayedAny := layerTwoClause.MatchString(afterChange)
			printedLint := regexp.MustCompile(`(?m)^lint: `).MatchString(afterChange)
			if scenario.layerTwo && printedLint && !replayedAny {
				t.Errorf("the run after the change served nothing from the findings cache, though its other files were unchanged:\n%s", afterChange)
			}
			if !scenario.layerTwo && replayedAny {
				t.Fatalf("the findings cache served files after a change to its key:\n%s", afterChange)
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

	// Changes nothing an eligible run prints can depend on. They used to be seen only by a scope fact in the
	// key, because the run printed git's changed set; it no longer asks git, and with no formatter its
	// format scope is a constant, so a replay here is correct. The proof is that it equals the cold run.
	for _, scenario := range []struct {
		name          string
		prepare, undo func()
		change        func()
	}{
		// A commit makes a changed file unchanged and moves no file's mtime.
		{"an uncommitted change committed",
			func() { write("source/a.ts", "export const a: number = 2;\n") },
			func() { write("source/a.ts", "export const a: number = 1;\n"); commit("restore") },
			func() { commit("commit the change") }},
		// docs exists and is tracked, and the build reads nothing in it, so no watched directory moves.
		{"an untracked file in a directory the build never reads", nil,
			func() { remove("docs/notes.md") },
			func() { write("docs/notes.md", "notes\n") }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.prepare != nil {
				scenario.prepare()
			}
			establishHit()
			scenario.change()
			defer scenario.undo()

			replayed, replayedExit := run(true)
			cold, coldExit := run(false)
			if isReplay(replayed) && (replayBody(replayed) != verdict(cold) || replayedExit != coldExit) {
				t.Fatalf("the replay is not the cold run's verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
					replayedExit, coldExit, replayBody(replayed), verdict(cold))
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

	// `--no-fix` is cached on its own key: it records and replays, and it never replays a bare run's
	// verdict or hands its own to one. The two print different reports, since `--no-fix` reports what a
	// writing run would rewrite, so either one replayed as the other would be a wrong report.
	t.Run("--no-fix is cached apart from a bare run", func(t *testing.T) {
		establishHit()
		if output, _ := run(true, "--no-fix"); isReplay(output) {
			t.Fatalf("the first --no-fix run replayed, so it was served the bare run's recording:\n%s", output)
		}
		replayed, replayedExit := run(true, "--no-fix")
		if !isReplay(replayed) {
			t.Fatalf("an unchanged --no-fix run did not replay its own recording:\n%s", replayed)
		}
		cold, coldExit := run(false, "--no-fix")
		if replayBody(replayed) != verdict(cold) || replayedExit != coldExit {
			t.Fatalf("the --no-fix replay is not the cold --no-fix verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
				replayedExit, coldExit, replayBody(replayed), verdict(cold))
		}
		if bare, _ := run(true); !isReplay(bare) {
			t.Fatalf("the bare run lost its own recording to the --no-fix one:\n%s", bare)
		}
	})

	// A cache table that cannot be trusted is thrown away whole, said so once, and replaced. Overwritten
	// rather than deleted, because a missing table is a first run and proves nothing about the discard.
	t.Run("a corrupt cache table is discarded, said so, and rewritten", func(t *testing.T) {
		establishHit()
		tables := []string{}
		filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && strings.HasPrefix(entry.Name(), "table-") && strings.HasSuffix(entry.Name(), ".gob") {
				tables = append(tables, path)
			}
			return nil
		})
		if len(tables) != 1 {
			t.Fatalf("expected one cache table under the isolated home, found %v", tables)
		}
		if dump, _ := run(true, "--cache-dump"); !strings.Contains(dump, "runs: ") || !strings.Contains(dump, "(bare): recorded ") {
			t.Fatalf("--cache-dump did not show the bare run it just recorded:\n%s", dump)
		}
		if err := os.WriteFile(tables[0], []byte("not a cache table"), 0o600); err != nil {
			t.Fatal(err)
		}
		if dump, _ := run(true, "--cache-dump"); !strings.Contains(dump, "cache table discarded") {
			t.Fatalf("--cache-dump printed a table this build would discard as though it were in use:\n%s", dump)
		}

		discarded, discardedExit := run(true)
		cold, coldExit := run(false)
		if isReplay(discarded) {
			t.Fatalf("a corrupt table replayed:\n%s", discarded)
		}
		if !strings.Contains(discarded, "note: cache table discarded") {
			t.Fatalf("the discard was silent, so a table thrown away every run would look merely cold:\n%s", discarded)
		}
		withoutNote := keepLines(discarded, "note: cache table discarded")
		if normalized(withoutNote) != normalized(cold) || discardedExit != coldExit {
			t.Fatalf("the run after the discard differs from a cold run:\n--- after discard\n%s\n--- cold\n%s", discarded, cold)
		}
		if again, _ := run(true); !isReplay(again) || strings.Contains(again, "discarded") {
			t.Fatalf("the run after the discard did not write a table the next run could replay:\n%s", again)
		}
	})
}
