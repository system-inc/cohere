package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// Every crash names itself, on the terminal and in `--json`: a count with no reason is a gap nobody can act
// on. A Swift lint-only run lost 1,251 files to one cause and the default view said only `⚠ 1251 files
// crashed`. These tests hold both engines to one shape: the human view groups crashes by cause, `--verbose`
// names every file on its own line and nothing more, and `--json` lists every crash beside the counts.

// crashEngine renders a lint phase that lost the given crashes, through one engine, in whatever view
// activeOutput holds.
type crashEngine struct {
	name   string
	render func(t *testing.T, crashes []runCrash) string
	// directory and extension are where the engine's files live, so a case's paths look like its own.
	directory, extension string
	// ruleCrashes is whether the engine can report one rule crashing on a file apart from a whole file lost.
	// The Swift contract carries only the second.
	ruleCrashes bool
	// swiftEngine is whether a crash's cause is the engine's own sentence, which the Swift engine's always is.
	swiftEngine bool
	// cause is a cause as the engine words one, and wantMessage the words every view must print for it.
	cause, wantMessage string
}

// swiftCrashSentence is the Swift engine's crash, worded as swift/Sources/CohereSwift/Pipeline/Linter.swift
// words it: never a panic, and it names what the file was not judged for.
const swiftCrashSentence = "no index store described it, so the rules that read what its names resolve to could not judge it " +
	"(cohere-swift/correctness-no-unused-declaration); every other rule ran on it"

var crashEngines = []crashEngine{
	{
		name: "TypeScript", render: renderTypeScriptCrashes, directory: "/project/source", extension: ".ts", ruleCrashes: true,
		cause: "index out of range",
		wantMessage: "cohere crashed on this file (index out of range). This is a bug in cohere, not in your code: " +
			"nothing in this file was checked. Please report it at " + reportBugsAt + " with the output of `cohere --version`, the rule, " +
			"the code that crashed it, and this message.",
	},
	{
		name: "Swift", render: renderSwiftCrashes, directory: "/project/Sources", extension: ".swift", swiftEngine: true,
		cause:       swiftCrashSentence,
		wantMessage: "the Swift engine could not finish this file: " + swiftCrashSentence,
	},
}

// renderTypeScriptCrashes runs the crashes through writeLintReport, as a walk's coverage names them.
func renderTypeScriptCrashes(t *testing.T, crashes []runCrash) string {
	t.Helper()
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"quiet-rule": 20},
		RulesListening: map[string]int{"quiet-rule": 20},
	}
	for _, crash := range crashes {
		if crash.Rule == "" {
			coverage.FilesCrashed = append(coverage.FilesCrashed, program.FileCrash{FileName: crash.Path, Cause: errors.New(crash.Cause)})
		} else {
			coverage.RulesCrashed = append(coverage.RulesCrashed, program.RuleCrash{RuleName: crash.Rule, FileName: crash.Path, Cause: errors.New(crash.Cause)})
		}
	}
	return renderLintReport(lintReport{Result: program.Result{Coverage: coverage}, Rules: []rule.Rule{{Name: "quiet-rule"}}, WalkCost: "in 1s"})
}

// renderSwiftCrashes runs the crashes through the Swift record stream: the contract's lint-only fixture with
// its lint record's crashes filled in, and its summary saying, as it must then, that the run is incomplete.
func renderSwiftCrashes(t *testing.T, crashes []runCrash) string {
	t.Helper()
	records := make([]map[string]string, 0, len(crashes))
	for _, crash := range crashes {
		records = append(records, map[string]string{"file": crash.Path, "error": crash.Cause})
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	stream := contractFixture(t, "LintOnly.jsonl")
	for index, line := range stream {
		line = strings.Replace(line, `"crashes":[]`, `"crashes":`+string(encoded), 1)
		stream[index] = strings.Replace(line, `"complete":true`, `"complete":false`, 1)
	}
	output, exitCode, err := renderRecords(t, swiftModeCheck, stream, 1)
	if err != nil || exitCode != 1 {
		t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
	}
	return output
}

// renderCrashesIn renders through the engine in the view asked for, from an empty summary, and returns what it
// printed and the summary it left. Both package-level settings are restored after.
func renderCrashesIn(t *testing.T, mode outputMode, engine crashEngine, crashes []runCrash) (string, runSummary) {
	t.Helper()
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	output := engine.render(t, crashes)
	return output, activeSummary
}

// crashedFilesOf is count whole files lost to one panic, named F01, F02 and on, so they sort as numbered.
func crashedFilesOf(engine crashEngine, prefix string, count int, cause string) []runCrash {
	crashes := make([]runCrash, 0, count)
	for index := range count {
		crashes = append(crashes, runCrash{
			Path: fmt.Sprintf("%s/%s%02d%s", engine.directory, prefix, index+1, engine.extension), Cause: cause, SwiftEngine: engine.swiftEngine,
		})
	}
	return crashes
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestADefaultRunNamesEachCrashGroupedByCause(t *testing.T) {
	for _, engine := range crashEngines {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		t.Run(engine.name+": one crash names its file and its cause", func(t *testing.T) {
			crash := runCrash{Path: engine.directory + "/Broken" + engine.extension, Cause: engine.cause, SwiftEngine: engine.swiftEngine}
			output, _ := renderCrashesIn(t, outputHuman, engine, []runCrash{crash})
			want := "  crashed: " + crash.Path + ": " + engine.wantMessage + "\n"
			if strings.Count(output, want) != 1 {
				t.Errorf("the default view did not name the crash once as\n%s\nin:\n%s", want, output)
			}
			forbidLines(t, output, "files, each:", "cohere --verbose names them all")
		})

		// Not parallel: it swaps the package-level activeOutput and activeSummary
		t.Run(engine.name+": twelve of one cause name it once, five files, and count the rest", func(t *testing.T) {
			shared := crashedFilesOf(engine, "F", 12, "index out of range")
			apart := runCrash{Path: engine.directory + "/Other" + engine.extension, Cause: "bad cast", SwiftEngine: engine.swiftEngine}
			output, _ := renderCrashesIn(t, outputHuman, engine, append(append([]runCrash(nil), shared...), apart))

			sharedMessage := shared[0].message()
			block := "  crashed: 12 files, each: " + sharedMessage + "\n"
			for _, crash := range shared[:5] {
				block += "    " + crash.Path + "\n"
			}
			block += "    and 7 more; cohere --verbose names them all\n" +
				"  crashed: " + apart.Path + ": " + apart.message() + "\n"
			if !strings.Contains(output, block) {
				t.Errorf("the default view did not group the crashes as\n%s\nin:\n%s", block, output)
			}
			if count := strings.Count(output, sharedMessage); count != 1 {
				t.Errorf("the shared cause printed %d times, want once:\n%s", count, output)
			}
			for _, crash := range shared[5:] {
				forbidLines(t, output, crash.Path)
			}
			if engine.name == "TypeScript" && output != block {
				// A TypeScript lint report prints nothing else in the default view, so the block is the whole of it.
				t.Errorf("the default view printed more than the crashes:\n%s", output)
			}
		})

		if !engine.ruleCrashes {
			continue
		}
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		t.Run(engine.name+": a rule's crash groups apart from the same panic losing a whole file", func(t *testing.T) {
			whole := crashedFilesOf(engine, "W", 2, "nil pointer")
			ruled := []runCrash{
				{Path: engine.directory + "/R01" + engine.extension, Rule: "prefer-arrow-callback", Cause: "nil pointer"},
				{Path: engine.directory + "/R02" + engine.extension, Rule: "prefer-arrow-callback", Cause: "nil pointer"},
			}
			output, _ := renderCrashesIn(t, outputHuman, engine, append(append([]runCrash(nil), whole...), ruled...))
			requireLines(t, output,
				"  crashed: 2 files, each: "+whole[0].message()+"\n    "+whole[0].Path+"\n    "+whole[1].Path+"\n",
				"  crashed: 2 files, each: "+ruled[0].message()+"\n    "+ruled[0].Path+"\n    "+ruled[1].Path+"\n",
			)
			if !strings.Contains(ruled[0].message(), "cohere's rule prefer-arrow-callback crashed") {
				t.Errorf("a rule crash's cause does not name its rule: %s", ruled[0].message())
			}
			forbidLines(t, output, "more; cohere --verbose")
		})
	}
}

// A Swift crash is never a panic: the engine reports a file its typed rules had no index for, and its sentence
// already says what that cost and that every other rule ran. So in both views it reads as the engine said it,
// and never as cohere's crash, which would claim nothing in the file was checked and call it a bug to report.
// A TypeScript crash is a panic, and keeps cohere's words.
// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestASwiftCrashSaysTheEnginesWordsInEveryView(t *testing.T) {
	cohereWords := []string{"bug in cohere", "nothing in this file was checked", reportBugsAt}
	for _, engine := range crashEngines {
		for _, mode := range []outputMode{outputHuman, outputVerbose} {
			view := map[outputMode]string{outputHuman: "default", outputVerbose: "verbose"}[mode]
			// Not parallel: it swaps the package-level activeOutput and activeSummary
			t.Run(engine.name+" "+view, func(t *testing.T) {
				crash := runCrash{Path: engine.directory + "/Broken" + engine.extension, Cause: engine.cause, SwiftEngine: engine.swiftEngine}
				output, _ := renderCrashesIn(t, mode, engine, []runCrash{crash})
				requireLines(t, output, "  crashed: "+crash.Path+": "+engine.wantMessage+"\n")
				if engine.swiftEngine {
					forbidLines(t, output, cohereWords...)
				} else {
					requireLines(t, output, cohereWords...)
				}
			})
		}
	}
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestVerboseNamesEveryCrashOnceAndNoGroup(t *testing.T) {
	for _, engine := range crashEngines {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		t.Run(engine.name, func(t *testing.T) {
			crashes := crashedFilesOf(engine, "F", 12, "index out of range")
			output, _ := renderCrashesIn(t, outputVerbose, engine, crashes)
			for _, crash := range crashes {
				line := "  crashed: " + crash.Path + ": " + crash.message() + "\n"
				if count := strings.Count(output, line); count != 1 {
					t.Errorf("--verbose named %s %d times, want once:\n%s", crash.Path, count, output)
				}
			}
			forbidLines(t, output, "files, each:", "cohere --verbose names them all")
		})
	}
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestJSONListsEveryCrashBesideTheCounts(t *testing.T) {
	for _, engine := range crashEngines {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		t.Run(engine.name, func(t *testing.T) {
			crashes := append(crashedFilesOf(engine, "F", 7, "index out of range"), crashedFilesOf(engine, "G", 1, "bad cast")...)
			if engine.ruleCrashes {
				crashes = append(crashes, runCrash{Path: engine.directory + "/R01" + engine.extension, Rule: "prefer-arrow-callback", Cause: "nil pointer"})
			}
			output, summary := renderCrashesIn(t, outputJSON, engine, crashes)
			forbidLines(t, output, "crashed: ")

			encoded, err := json.Marshal(summaryAsJSON(summary))
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Gaps struct {
					CrashedFiles int                 `json:"crashedFiles"`
					RuleCrashes  int                 `json:"ruleCrashes"`
					Crashes      []map[string]string `json:"crashes"`
				} `json:"gaps"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Gaps.Crashes) != decoded.Gaps.CrashedFiles+decoded.Gaps.RuleCrashes {
				t.Errorf("--json lists %d crashes and counts %d crashed files and %d rule crashes:\n%s",
					len(decoded.Gaps.Crashes), decoded.Gaps.CrashedFiles, decoded.Gaps.RuleCrashes, encoded)
			}
			want := make([]map[string]string, 0, len(crashes))
			for _, crash := range crashes {
				entry := map[string]string{"path": crash.Path, "cause": crash.Cause}
				if crash.Rule != "" {
					entry["rule"] = crash.Rule
				}
				want = append(want, entry)
			}
			got := decoded.Gaps.Crashes
			sort.Slice(got, func(first, second int) bool { return got[first]["path"] < got[second]["path"] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("--json lists the crashes as %v, want %v:\n%s", got, want, encoded)
			}
		})
	}
}
