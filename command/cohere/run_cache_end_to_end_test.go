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
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain gives every binary this package's tests launch a user cache of its own.
//
// Child processes inherit the test process's environment, so without this every test that runs the
// binary would read and write the developer's real cache table. Each fixture is a fresh directory and
// the table is keyed by the project root, so no two tests share a table; a test that needs a cold run
// says `--no-cache`, and TestRunCacheEndToEnd sets a home of its own for its runs.
func TestMain(suite *testing.M) {
	// In-process, the tests read the verbose account, as the binary tests do through verboseArguments.
	activeOutput = outputSettings{Mode: outputVerbose}
	pinGoEnvironment()
	home, temporaryDirectoryError := os.MkdirTemp("", "cohere-test-home-")
	if temporaryDirectoryError != nil {
		panic(temporaryDirectoryError)
	}
	// Ignored: Setenv fails only for a name holding "=" or a NUL, and neither name here does.
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("XDG_CACHE_HOME", "") // Ignored: as the line above.
	// See held_output_guard_test.go: a command whose output never closes fails the binary with its holder
	// named, rather than at -timeout with nothing said.
	stopWatching := watchForHeldOutput(heldOutputDeadline, 10*time.Second, failHeldOutput)
	code := suite.Run()
	stopWatching()
	// Ignored: a temporary directory of this test binary's own, which costs only its space if it outlives the
	// run, and the run's verdict is already decided. So for each removal below.
	_ = os.RemoveAll(home)
	if sharedBinary.directory != "" {
		_ = os.RemoveAll(sharedBinary.directory) // Ignored: as home's, above.
	}
	for _, stamped := range stampedBinaries.byVersion {
		if stamped.directory != "" {
			_ = os.RemoveAll(stamped.directory) // Ignored: as home's, above.
		}
	}
	os.Exit(code)
}

// nodeCounts is the lint line's node account: what the walk visited, and what the cache covered by replay.
var nodeCounts = regexp.MustCompile(`(\d+) nodes visited(?:, (\d+) more covered by replay)?`)

// foldNodeCounts writes each lint line's node account as one total, the form a warm run and a cold run agree on:
// the warm one walked fewer nodes and replayed the rest (#kdee854).
func foldNodeCounts(output string) string {
	return nodeCounts.ReplaceAllStringFunc(output, func(account string) string {
		parts := nodeCounts.FindStringSubmatch(account)
		// Ignored: the pattern captures digits.
		visited, _ := strconv.Atoi(parts[1])
		// Ignored: digits, or nothing when no file was replayed, which reads as the 0 it means.
		replayed, _ := strconv.Atoi(parts[2])
		return fmt.Sprintf("%d nodes visited", visited+replayed)
	})
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
func TestRunCacheEndToEnd(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	// Gigabytes too: the memory line reads available memory live, so two runs a second apart differ there.
	durations := regexp.MustCompile(`\d+(\.\d+)?(ms|s|µs| GB)\b`)
	// The findings cache's clause says how much of the verdict was remembered; a cold run never has it,
	// so it comes off before a comparison and is required or forbidden separately per scenario.
	// The types phase's clause says how many files' semantic diagnostics were replayed; a cold run never has it.
	typesClause := regexp.MustCompile(`; \d+ of \d+ files' semantic diagnostics replayed from cache`)
	layerTwoClause := regexp.MustCompile(
		`; \d+ of \d+ files replayed from cache( \(type-aware rules ran again on \d+ of them, shape-keyed on \d+\))?( \(design-system rules ran again on \d+ of them\))?( \(derived rules ran again on \d+ of them\))?`,
	)
	// A cold run is a `--no-cache` run, which says so in a line no cached run prints.
	cacheOffLine := regexp.MustCompile(`(?m)^  cache: off, by --no-cache.*\n`)
	// A cached run says how many shapes it keyed on content (#5txm9gg); a cold run keys none, so the line is
	// the invocation's, not the tree's.
	contentKeyedLine := regexp.MustCompile(`(?m)^  cache: \d+ files?'s? shapes? (is|are) keyed on content.*\n?`)
	// A run the cache did not answer whole says why (#547dhjz); a cold run reads no cache, so it never has the
	// line, which is the invocation's rather than the tree's.
	missLine := regexp.MustCompile(`(?m)^cache: (not replayed whole|findings): .*\n?`)
	// The total line's shape depends on timing as well as its numbers: the types phase's check runs alongside
	// the fix walk, so whether the phases overlap is a property of this invocation, never of the tree.
	totalLine := regexp.MustCompile(`(?m)^  total .*\n`)
	// The footer is this invocation's: its time, and how many files it checked fresh against how many the
	// cache answered for, which a warm run and a cold one differ in by design.
	footerLine := regexp.MustCompile(`(?m)^(✓ 💎|✗ ☠️) .*\n?`)
	normalized := func(output string) string {
		folded := foldNodeCounts(missLine.ReplaceAllString(contentKeyedLine.ReplaceAllString(output, ""), ""))
		return typesClause.ReplaceAllString(footerLine.ReplaceAllString(totalLine.ReplaceAllString(
			cacheOffLine.ReplaceAllString(
				layerTwoClause.ReplaceAllString(durations.ReplaceAllString(folded, "T"), ""),
				"",
			),
			"",
		), ""), "")
	}
	keepLines := func(output string, drop ...string) string {
		kept := []string{}
		for _, line := range strings.Split(output, "\n") {
			if slices.ContainsFunc(drop, func(prefix string) bool { return strings.HasPrefix(line, prefix) }) {
				continue
			}
			kept = append(kept, line)
		}
		return strings.Join(kept, "\n")
	}
	// A replay must equal the cold run's verdict: its lines minus the ones that describe that
	// invocation, which a replay deliberately does not print. Its own framing comes off first. The footer is
	// the invocation's too, with its own time, and a replay renders its own (required below).
	verdict := func(cold string) string {
		return keepLines(
			cold,
			"graph built in ",
			"types: ",
			"lint: ",
			"phases: ",
			"  total ",
			"  memory: ",
			"  cache: off, by --no-cache",
			"✓ 💎 ",
			"✗ ☠️ ",
		)
	}
	// A replay says which run a phase's line came from; that label comes off before the comparison,
	// and is required separately below so a replay that lost it fails.
	provenance := regexp.MustCompile(
		`^(fix|format scope|nested repositories) \(from the cached run at \d\d:\d\d:\d\d\): `,
	)
	replayBody := func(replay string) string {
		lines := strings.Split(keepLines(
			contentKeyedLine.ReplaceAllString(replay, ""),
			"cached: ",
			"phases: replayed ",
			"  this run: ",
			"  memory: ",
			"✓ 💎 ",
			"✗ ☠️ ",
		), "\n")
		for index, line := range lines {
			lines[index] = provenance.ReplaceAllString(line, "$1: ")
		}
		return strings.Join(lines, "\n")
	}

	// layerTwo says whether the run after the change should serve unchanged files from the findings
	// cache. A change to the lint config or the tsconfig changes the cache's key, so nothing may be
	// replayed; any other change leaves the untouched files replayable.
	scenarios := []struct {
		name     string
		layerTwo bool
		prepare  func(fixture *runCacheFixture)
		change   func(fixture *runCacheFixture)
	}{
		// Type errors rather than lint findings: a finding with a fixer is rewritten by the run that
		// sees it, which is the fix-run case below and a different property.
		{"a source file edited to add a finding", true, nil,
			func(fixture *runCacheFixture) { fixture.write("source/a.ts", "export const a: number = \"x\";\n") }},
		// The same size and the old modification time, as cp -p, rsync -t and touch -r leave a file: only
		// the change time and the bytes moved.
		{"a same-size edit with its modification time restored", true, nil,
			func(fixture *runCacheFixture) {
				information, statError := os.Stat(filepath.Join(fixture.root, "source/a.ts"))
				if statError != nil {
					fixture.test.Fatal(statError)
				}
				time.Sleep(10 * time.Millisecond)
				fixture.write("source/a.ts", "export const a: string = 1;\n")
				if changeTimesError := os.Chtimes(
					filepath.Join(fixture.root, "source/a.ts"),
					information.ModTime(),
					information.ModTime(),
				); changeTimesError != nil {
					fixture.test.Fatal(changeTimesError)
				}
			}},
		{"a file added at the top level", true, nil,
			func(fixture *runCacheFixture) { fixture.write("source/z.ts", "export const z: number = \"x\";\n") }},
		// The case the design first missed: a file added one directory down moves only that
		// directory's mtime.
		{"a file added in a nested directory", true, nil,
			func(fixture *runCacheFixture) {
				fixture.write("source/nested/y.ts", "export const y: number = \"x\";\n")
			}},
		{"a file deleted", true, nil,
			func(fixture *runCacheFixture) { fixture.remove("source/nested/b.ts") }},
		{"the lint config changed", false, nil,
			func(fixture *runCacheFixture) {
				fixture.write(
					"CohereSettings.json",
					`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error","prefer-const":"error"}}`,
				)
			}},
		// Only the base is edited, and it is untracked, so git reports the same changed files before and
		// after and the scope fact cannot see it. The run cache's declared inputs and the findings
		// cache's key must each name every file in the extends chain, or both replay the old verdict.
		{"a base the lint config extends changed", false,
			func(fixture *runCacheFixture) {
				fixture.write(
					"lint/base.json",
					`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error"}}`,
				)
				fixture.write(
					"CohereSettings.json",
					`{"extends":"./lint/base.json","rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off"}}`,
				)
			},
			func(fixture *runCacheFixture) {
				fixture.write(
					"lint/base.json",
					`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error","prefer-const":"error"}}`,
				)
			}},
		// The two above under a config that names its sets, which is read on its own goroutine beside the graph
		// build (lintConfigAhead) rather than after it. The read moved; the declaration of what it read did not,
		// and these show the run cache still names the config and the base it extends on that path.
		{"the lint config changed, on a config read ahead of the build", false,
			func(fixture *runCacheFixture) {
				fixture.write(
					"CohereSettings.json",
					`{"extends":["cohere:react"],"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error"}}`,
				)
				fixture.requireConfigReadAhead()
			},
			func(fixture *runCacheFixture) {
				fixture.write(
					"CohereSettings.json",
					`{"extends":["cohere:react"],"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error","prefer-const":"error"}}`,
				)
			}},
		{"a base the lint config extends changed, on a config read ahead of the build", false,
			func(fixture *runCacheFixture) {
				// A base beside a set may configure only rules no set in the chain does, so it holds one of its own.
				fixture.write("lint/base.json", `{"rules":{"no-console":"off"}}`)
				fixture.write(
					"CohereSettings.json",
					`{"extends":["cohere:react","./lint/base.json"],"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off"}}`,
				)
				fixture.requireConfigReadAhead()
			},
			func(fixture *runCacheFixture) {
				fixture.write("lint/base.json", `{"rules":{"no-console":"error"}}`)
			}},
		{"the tsconfig changed", false, nil,
			func(fixture *runCacheFixture) {
				fixture.write(
					"tsconfig.json",
					`{"compilerOptions":{"strict":false,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`,
				)
			}},
		{"package.json edited in place", true, nil,
			func(fixture *runCacheFixture) {
				fixture.write("package.json", `{"name":"fixture","private":true,"type":"commonjs"}`)
			}},
		// node_modules is ignored, so git never mentions it and the scope fact cannot see this. Only
		// the input recorder can: the build read this declaration, so its signature is an input.
		{"a dependency's declaration changed", true,
			func(fixture *runCacheFixture) {
				fixture.write("node_modules/dep/package.json", `{"name":"dep","types":"index.d.ts"}`)
				fixture.write("node_modules/dep/index.d.ts", "export declare const d: number;\n")
				fixture.write("source/u.ts", "import { d } from \"dep\";\nexport const u: number = d;\n")
			},
			func(fixture *runCacheFixture) {
				fixture.write("node_modules/dep/index.d.ts", "export declare const d: string;\n")
			}},
	}

	for _, scenario := range scenarios {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newRunCacheFixture(test, binary)
			if scenario.prepare != nil {
				scenario.prepare(fixture)
			}
			fixture.establishHit()
			scenario.change(fixture)

			afterChange, afterChangeExit := fixture.run(true)
			cold, coldExit := fixture.run(false)
			if isRunCacheReplay(afterChange) {
				test.Fatalf("the change was not noticed: the recorded run was replayed as current:\n%s", afterChange)
			}
			// The clause rides on the lint line, and a run whose types bail prints no lint line at all, as a
			// cold run does not either: the walk still replayed, but no lint verdict is reported to be
			// remembered. So it is required only where a lint line was printed.
			replayedAny := layerTwoClause.MatchString(afterChange)
			printedLint := regexp.MustCompile(`(?m)^lint: `).MatchString(afterChange)
			if scenario.layerTwo && printedLint && !replayedAny {
				test.Errorf(
					"the run after the change served nothing from the findings cache, though its other files were unchanged:\n%s",
					afterChange,
				)
			}
			if !scenario.layerTwo && replayedAny {
				test.Fatalf("the findings cache served files after a change to its key:\n%s", afterChange)
			}
			if normalized(afterChange) != normalized(cold) || afterChangeExit != coldExit {
				test.Fatalf(
					"the run after the change differs from a cold run (exit %d against %d):\n--- after the change\n%s\n--- cold\n%s",
					afterChangeExit,
					coldExit,
					afterChange,
					cold,
				)
			}

			next, nextExit := fixture.run(true)
			if !isRunCacheReplay(next) {
				test.Fatalf("the run after that did not replay the new truth:\n%s", next)
			}
			// Under zero config the sets line comes first, and it replays as it was: which sets applied is the
			// tree's verdict, not this run's.
			if !provenance.MatchString(keepLines(next, "cached: ", "sets: ")) {
				test.Fatalf("the replay printed the fix line as though the fix phase had just run:\n%s", next)
			}
			if replayBody(next) != verdict(cold) || nextExit != coldExit {
				test.Fatalf(
					"the replay is not the cold run's verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
					nextExit,
					coldExit,
					replayBody(next),
					verdict(cold),
				)
			}
			// And it ends on a footer of its own, which says it was replayed rather than claiming the cold run's.
			if !regexp.MustCompile(`(?m)^(✓ 💎|✗ ☠️) [0-9.]+s(?: •[^\n]*)? • replayed`).MatchString(next) {
				test.Fatalf("the replay did not end on a footer saying it was replayed:\n%s", next)
			}
		})
	}

	// Changes nothing an eligible run prints can depend on. They used to be seen only by a scope fact in the
	// key, because the run printed git's changed set; it no longer asks git, and with no formatter its
	// format scope is a constant, so a replay here is correct. The proof is that it equals the cold run.
	for _, scenario := range []struct {
		name    string
		prepare func(fixture *runCacheFixture)
		change  func(fixture *runCacheFixture)
	}{
		// A commit makes a changed file unchanged and moves no file's mtime.
		{"an uncommitted change committed",
			func(fixture *runCacheFixture) { fixture.write("source/a.ts", "export const a: number = 2;\n") },
			func(fixture *runCacheFixture) { fixture.commit("commit the change") }},
		// docs exists and is tracked, and the build reads nothing in it, so no watched directory moves.
		{"an untracked file in a directory the build never reads", nil,
			func(fixture *runCacheFixture) { fixture.write("docs/notes.md", "notes\n") }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			fixture := newRunCacheFixture(test, binary)
			if scenario.prepare != nil {
				scenario.prepare(fixture)
			}
			fixture.establishHit()
			scenario.change(fixture)

			replayed, replayedExit := fixture.run(true)
			cold, coldExit := fixture.run(false)
			if isRunCacheReplay(replayed) && (replayBody(replayed) != verdict(cold) || replayedExit != coldExit) {
				test.Fatalf(
					"the replay is not the cold run's verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
					replayedExit,
					coldExit,
					replayBody(replayed),
					verdict(cold),
				)
			}
		})
	}

	// A run whose fix phase rewrites a file is never replayed. The record step stats inputs after the
	// rewrite, so without the decline the next run would match the fixed tree and replay "1 of 1 files
	// rewritten" over a tree it never touched.
	test.Run("a fix run is not replayed", func(test *testing.T) {
		test.Parallel()
		fixture := newRunCacheFixture(test, binary)
		fixture.establishHit()
		fixture.write("source/a.ts", "export const a: number = 1;\ndebugger;\n")

		fixRun, _ := fixture.run(true)
		if !strings.Contains(fixRun, "1 of 1 files rewritten") {
			test.Fatalf("the fix run rewrote nothing, so this case proves nothing:\n%s", fixRun)
		}
		next, _ := fixture.run(true)
		cold, _ := fixture.run(false)
		if strings.Contains(next, "files rewritten, 1 fixes applied") {
			test.Fatalf("the rewrite was replayed over a tree nothing touched:\n%s", next)
		}
		if normalized(next) != normalized(cold) {
			test.Fatalf("the run after the fix differs from a cold run:\n--- next\n%s\n--- cold\n%s", next, cold)
		}
	})

	// `--no-fix` is cached on its own key: it records and replays, and it never replays a bare run's
	// verdict or hands its own to one. The two print different reports, since `--no-fix` reports what a
	// writing run would rewrite, so either one replayed as the other would be a wrong report.
	test.Run("--no-fix is cached apart from a bare run", func(test *testing.T) {
		test.Parallel()
		fixture := newRunCacheFixture(test, binary)
		fixture.establishHit()
		if output, _ := fixture.run(true, "--no-fix"); isRunCacheReplay(output) {
			test.Fatalf("the first --no-fix run replayed, so it was served the bare run's recording:\n%s", output)
		}
		replayed, replayedExit := fixture.run(true, "--no-fix")
		if !isRunCacheReplay(replayed) {
			test.Fatalf("an unchanged --no-fix run did not replay its own recording:\n%s", replayed)
		}
		cold, coldExit := fixture.run(false, "--no-fix")
		if replayBody(replayed) != verdict(cold) || replayedExit != coldExit {
			test.Fatalf(
				"the --no-fix replay is not the cold --no-fix verdict (exit %d against %d):\n--- replay\n%s\n--- cold verdict\n%s",
				replayedExit,
				coldExit,
				replayBody(replayed),
				verdict(cold),
			)
		}
		if bare, _ := fixture.run(true); !isRunCacheReplay(bare) {
			test.Fatalf("the bare run lost its own recording to the --no-fix one:\n%s", bare)
		}
	})

	// A cache table that cannot be trusted is thrown away, file by file, said so once, and replaced.
	// Overwritten rather than deleted, because a missing table is a first run and proves nothing about the
	// discard.
	test.Run("a corrupt cache table is discarded, said so, and rewritten", func(test *testing.T) {
		test.Parallel()
		fixture := newRunCacheFixture(test, binary)
		fixture.establishHit()
		// The table is the project's own, and the isolated fixture.home holds none.
		tables := cacheTableFiles(test, fixture.root)
		if len(tables) == 0 {
			test.Fatalf("no cache table in the project %s", fixture.root)
		}
		// A walk that cannot read the home fails the test, rather than reading as a home with no table in it.
		walkError := filepath.WalkDir(fixture.home, func(path string, entry os.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".gob") {
				test.Errorf("a cache table landed in the user's fixture.home: %s", path)
			}
			return nil
		})
		if walkError != nil {
			test.Fatal(walkError)
		}
		// Every run here leaves formatting out (see run), so the bare run is recorded as `--no-format`.
		if dump, _ := fixture.run(
			true,
			"--cache-dump",
		); !strings.Contains(dump, "runs: ") || !strings.Contains(dump, "--no-format: recorded ") {
			test.Fatalf("--cache-dump did not show the bare run it just recorded:\n%s", dump)
		}
		for _, table := range tables {
			if writeError := os.WriteFile(table, []byte("not a cache table"), 0o600); writeError != nil {
				test.Fatal(writeError)
			}
		}
		if dump, _ := fixture.run(true, "--cache-dump"); !strings.Contains(dump, "cache table discarded") {
			test.Fatalf("--cache-dump printed a table this build would discard as though it were in use:\n%s", dump)
		}

		discarded, discardedExit := fixture.run(true)
		cold, coldExit := fixture.run(false)
		if isRunCacheReplay(discarded) {
			test.Fatalf("a corrupt table replayed:\n%s", discarded)
		}
		if !strings.Contains(discarded, "note: cache table discarded") {
			test.Fatalf(
				"the discard was silent, so a table thrown away every run would look merely cold:\n%s",
				discarded,
			)
		}
		withoutNote := keepLines(discarded, "note: cache table discarded")
		if normalized(withoutNote) != normalized(cold) || discardedExit != coldExit {
			test.Fatalf(
				"the run after the discard differs from a cold run:\n--- after discard\n%s\n--- cold\n%s",
				discarded,
				cold,
			)
		}
		if again, _ := fixture.run(true); !isRunCacheReplay(again) || strings.Contains(again, "discarded") {
			test.Fatalf("the run after the discard did not write a table the next run could replay:\n%s", again)
		}
	})
}

// TestFindingsCacheKeysNoHtmlLinkForPagesOnItsConfiguredPagesDirectory is the derived class's proof through the
// real binary, on the rule whose option chooses what it reads (#s9k38p3). @next/next/no-html-link-for-pages is
// configured with pagesDir `custom`, and `pages/team.tsx` stands where the default would read it, so a run that
// ignored the option would report the anchor from the start. An edit to a file nothing imports replays the
// anchor's file with its derived rule, and a page added under `custom` runs that rule again on every replayed
// file and reports the anchor, whose own bytes never changed. Each run is held against a cold one.
func TestFindingsCacheKeysNoHtmlLinkForPagesOnItsConfiguredPagesDirectory(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture := newRunCacheFixture(test, binary)
	fixture.write(
		"tsconfig.json",
		`{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","jsx":"preserve","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`,
	)
	fixture.write(
		"CohereSettings.json",
		`{"plugins":["@next"],"rules":{"nexus/consistency-no-ambiguous-identifier":"off",`+
			`"@typescript-eslint/no-inferrable-types":"off",`+
			`"no-debugger":"error","@next/next/no-html-link-for-pages":["error","custom"]}}`,
	)
	// Without React's types an intrinsic element is a type error, and a run whose types bail never lints.
	fixture.write(
		"source/jsx.d.ts",
		"declare namespace JSX {\n    interface IntrinsicElements {\n        [elementName: string]: unknown;\n    }\n}\n",
	)
	fixture.write("source/link.tsx", "export const TeamLink = () => <a href=\"/team\">Team</a>;\n")
	fixture.write("custom/about.tsx", "export {};\n")
	fixture.write("pages/team.tsx", "export {};\n")
	fixture.commit("a pages directory configured")

	durations := regexp.MustCompile(`\d+(\.\d+)?(ms|s|µs| GB)\b`)
	lintLine := regexp.MustCompile(`(?m)^lint: .*$`)
	finding := "@next/next/no-html-link-for-pages"
	// The lint line and the footer are the invocation's; every other line is the tree's verdict.
	verdict := func(output string) string {
		withoutLintLine := lintLine.ReplaceAllString(durations.ReplaceAllString(output, "T"), "")
		kept := []string{}
		for _, line := range strings.Split(withoutLintLine, "\n") {
			if !strings.HasPrefix(line, "  ") &&
				!strings.HasPrefix(line, "✓") &&
				!strings.HasPrefix(line, "✗") &&
				!strings.HasPrefix(line, "cache: ") &&
				!strings.HasPrefix(line, "graph built in ") &&
				!strings.HasPrefix(line, "types: ") &&
				!strings.HasPrefix(line, "phases: ") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}

	fixture.establishHit("--no-fix")
	if output, _ := fixture.run(false, "--no-fix"); strings.Contains(output, finding) {
		test.Fatalf("the anchor reports before any page under custom names it, so the option was not read:\n%s", output)
	}

	// An edit elsewhere: the anchor's file replays, its derived rule with it. The edited file is one nothing
	// imports, since other derived rules read types and rightly run again on an edited file's importers, which
	// would count here though this rule replayed.
	fixture.write("source/nested/b.ts", "import { a } from \"../a\";\nexport const b = a + 2;\n")
	warm, warmExit := fixture.run(true, "--no-fix")
	cold, coldExit := fixture.run(false, "--no-fix")
	if isRunCacheReplay(warm) || !strings.Contains(lintLine.FindString(warm), "files replayed from cache") {
		test.Fatalf("an edit to b.ts served nothing from the findings cache:\n%s", warm)
	}
	if strings.Contains(warm, "derived rules ran again") {
		test.Errorf("an edit outside the routes ran the derived rule again on replayed files:\n%s", warm)
	}
	if verdict(warm) != verdict(cold) || warmExit != coldExit {
		test.Fatalf(
			"the warm run differs from a cold one (exit %d against %d):\n--- warm\n%s\n--- cold\n%s",
			warmExit,
			coldExit,
			warm,
			cold,
		)
	}

	// A page added under the configured directory: no file the rule checks changed, and the anchor now reports.
	fixture.write("custom/team.tsx", "export {};\n")
	warm, warmExit = fixture.run(true, "--no-fix")
	cold, coldExit = fixture.run(false, "--no-fix")
	if isRunCacheReplay(warm) {
		test.Fatalf(
			"a page added under the configured directory was not noticed, and the run was replayed whole:\n%s",
			warm,
		)
	}
	if !strings.Contains(cold, finding) {
		test.Fatalf(
			"a cold run does not report the anchor after its page was added, so this case proves nothing:\n%s",
			cold,
		)
	}
	if !strings.Contains(lintLine.FindString(warm), "derived rules ran again") {
		test.Errorf(
			"a page added under the configured directory did not run the derived rule again on the replayed files:\n%s",
			warm,
		)
	}
	if verdict(warm) != verdict(cold) || warmExit != coldExit {
		test.Fatalf(
			"after a page was added the warm run differs from a cold one (exit %d against %d):\n--- warm\n%s\n--- cold\n%s",
			warmExit,
			coldExit,
			warm,
			cold,
		)
	}
}

// runCacheFixture is a repository TestRunCacheEndToEnd's scenarios run cohere in: a small project committed
// once, with a home of its own. Each scenario has its own, so they run in parallel; on one shared
// repository they ran one after another, each undoing its change for the next (#nxgt2ca).
type runCacheFixture struct {
	test   *testing.T
	binary string
	root   string
	home   string
}

func newRunCacheFixture(test *testing.T, binary string) *runCacheFixture {
	test.Helper()
	fixture := &runCacheFixture{test: test, binary: binary, root: test.TempDir(), home: test.TempDir()}
	fixture.write(
		"tsconfig.json",
		`{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler","incremental":true,"tsBuildInfoFile":".cache/ts/tsconfig.tsbuildinfo"},"include":["source"]}`,
	)
	fixture.write(
		"CohereSettings.json",
		`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":"error","no-var":"error"}}`,
	)
	fixture.write("package.json", `{"name":"fixture","private":true,"type":"module"}`)
	fixture.write(".gitignore", ".cache/\nnode_modules/\n")
	fixture.write("source/a.ts", "export const a: number = 1;\n")
	fixture.write("source/nested/b.ts", "import { a } from \"../a\";\nexport const b = a + 1;\n")
	fixture.write("docs/keep.md", "keep\n")
	fixture.git("init", "--quiet")
	fixture.git("config", "user.email", "fixture@example.com")
	fixture.git("config", "user.name", "fixture")
	fixture.commit("initial")
	return fixture
}

func (fixture *runCacheFixture) git(arguments ...string) {
	fixture.test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = fixture.root
	command.Env = append(os.Environ(), "HOME="+fixture.home)
	if output, outputError := command.CombinedOutput(); outputError != nil {
		fixture.test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
	}
}

func (fixture *runCacheFixture) write(name, contents string) {
	fixture.test.Helper()
	path := filepath.Join(fixture.root, name)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		fixture.test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		fixture.test.Fatal(writeError)
	}
}

// requireConfigReadAhead fails the scenario unless the fixture's lint config is one startLintConfigAhead reads
// beside the build. A config that names no set is zero config, read after the build as it always was, so a
// scenario meant for the ahead path would pass on the other one and prove nothing about it.
func (fixture *runCacheFixture) requireConfigReadAhead() {
	fixture.test.Helper()
	if usesHouseSets(projectLocation{LintConfigFileName: filepath.Join(fixture.root, "CohereSettings.json")}) {
		fixture.test.Fatal("the fixture's lint config is zero config, so it is read after the build, not ahead of it")
	}
}

func (fixture *runCacheFixture) remove(name string) {
	fixture.test.Helper()
	if removeError := os.RemoveAll(filepath.Join(fixture.root, name)); removeError != nil {
		fixture.test.Fatal(removeError)
	}
}

func (fixture *runCacheFixture) commit(message string) {
	fixture.test.Helper()
	fixture.git("add", "-A")
	fixture.git("commit", "--quiet", "--allow-empty", "-m", message)
}

// run launches the binary bare unless arguments are given; bare and `--no-fix` are the shapes the
// run cache serves. cached false is the cold truth every cached result is held against.
func (fixture *runCacheFixture) run(cached bool, arguments ...string) (string, int) {
	fixture.test.Helper()
	command := exec.Command(fixture.binary, verboseArguments(arguments)...)
	command.Dir = fixture.root
	environment := []string{"HOME=" + fixture.home}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "HOME=") {
			environment = append(environment, variable)
		}
	}
	if !cached {
		command.Args = append(command.Args, "--no-cache")
	}
	// What the cache notices is the subject here, not formatting: the fixture's JSON is written unformatted
	// on purpose, and a run that formats it is a run that writes, which is never recorded.
	command.Args = append(command.Args, "--no-format")
	command.Env = environment
	output, outputError := command.CombinedOutput()
	if outputError == nil {
		return string(output), 0
	}
	exitError, isExit := outputError.(*exec.ExitError)
	if !isExit {
		fixture.test.Fatalf("running cohere: %v\n%s", outputError, output)
	}
	return string(output), childExitCode(fixture.test, exitError)
}

// establishHit runs twice with arguments and requires the second to replay the first.
func (fixture *runCacheFixture) establishHit(arguments ...string) {
	fixture.test.Helper()
	fixture.run(true, arguments...)
	if output, _ := fixture.run(true, arguments...); !isRunCacheReplay(output) {
		fixture.test.Fatalf(
			"an unchanged tree did not replay, so nothing below can show a change was noticed:\n%s",
			output,
		)
	}
}

// isRunCacheReplay reports a run the cache answered: a replay says so on its first line.
func isRunCacheReplay(output string) bool { return strings.HasPrefix(output, "cached: ") }

// pinGoEnvironment sets the go command's caches and settings in the environment as the caller has them, before
// TestMain moves `HOME`. The go command finds its build cache, its module cache and its settings file under
// `HOME`, so every test that built this command under the fake home compiled the whole module cold and lost
// the house's -trimpath: about a minute per package run, the most of command/cohere's wall (#nxgt2ca). A
// variable the caller already set is left as it is.
func pinGoEnvironment() {
	output, outputError := exec.Command("go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH", "GOFLAGS").Output()
	if outputError != nil {
		panic(fmt.Sprintf("reading the go environment: %v", outputError))
	}
	var settings map[string]string
	if decodeError := json.Unmarshal(output, &settings); decodeError != nil {
		panic(fmt.Sprintf("reading the go environment: %v", decodeError))
	}
	for name, value := range settings {
		if _, set := os.LookupEnv(name); !set && value != "" {
			// Ignored: Setenv fails only for a name holding "=" or a NUL, and go env names none that way.
			_ = os.Setenv(name, value)
		}
	}
}

// TestAReplayThatCannotBeWrittenFailsTheRun: a replay whose stdout cannot be written exits nonzero and says so,
// as a run that computed its report does. A replay used to write straight to the terminal's files and drop every
// error, so the review's `cohere --no-fix 1</dev/null`, run three times, exited 1 on the miss and 0 on both
// replays, with nothing said. Here stdout is a file opened for reading only, which every write fails on, as on
// that descriptor.
func TestAReplayThatCannotBeWrittenFailsTheRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json": fixScopeTsconfig,
		".gitignore":    ".cache/\n",
		"Clean.ts":      "export const clean = 1;\n",
	})
	arguments := []string{"--no-fix", "--no-format", "--verbose"}
	unwritable, openError := os.Open(filepath.Join(root, "Clean.ts"))
	if openError != nil {
		test.Fatal(openError)
	}
	// Ignored: opened only to be handed to the child as an output it cannot write, and never written here.
	defer unwritable.Close()

	// The first is the miss, which records the run; the other two replay it.
	for _, run := range []string{"the miss", "the first replay", "the second replay"} {
		var standardError bytes.Buffer
		command := exec.Command(binary, arguments...)
		command.Dir = root
		command.Stdout, command.Stderr = unwritable, &standardError
		runError := command.Run()
		var exitError *exec.ExitError
		if !errors.As(runError, &exitError) || childExitCode(test, exitError) == 0 {
			test.Fatalf("%s, into output it could not write, ended with %v, want a nonzero exit:\n%s", run, runError,
				standardError.String())
		}
		if !strings.Contains(standardError.String(), "Writing the output failed") {
			test.Errorf("%s does not say its output did not arrive:\n%s", run, standardError.String())
		}
	}
	// And the runs above had a recorded run to replay: the tree, unchanged, replays now.
	if output, _ := runCohere(test, binary, root, arguments...); !isRunCacheReplay(output) {
		test.Fatalf("an unchanged tree did not replay, so the runs above may not have been replays:\n%s", output)
	}
}
