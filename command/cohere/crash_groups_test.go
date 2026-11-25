package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/typescript_language/program"
	"github.com/system-inc/cohere/internal/typescript_language/rule"
)

// Every crash names itself, on the terminal and in `--json`: a count with no reason is a gap nobody can act
// on. A Swift lint-only run lost 1,251 files to one cause and the default view said only `⚠ 1251 files
// crashed`. These tests hold both languages to one shape: the human view groups crashes by cause, `--verbose`
// names every file on its own line and nothing more, and `--json` lists every crash beside the counts.

// crashLanguage renders a lint phase that lost the given crashes, through one child, in whatever view
// activeOutput holds.
type crashLanguage struct {
	name   string
	render func(test *testing.T, crashes []runCrash) string
	// directory and extension are where the child's files live, so a case's paths look like its own.
	directory, extension string
	// ruleCrashes is whether the child can report one rule crashing on a file apart from a whole file lost.
	// The contract carries only the second.
	ruleCrashes bool
	// language names the contract language whose crash's cause is its own sentence, as runCrash.Language does, and
	// is empty for TypeScript, whose crash is a panic.
	language string
	// cause is a cause as the child words one, and wantMessage the words every view must print for it.
	cause, wantMessage string
}

// swiftCrashSentence is cohere-swift's crash, worded as swift_language/Sources/CohereSwift/Pipeline/Linter.swift
// words it: never a panic, and it names what the file was not judged for.
const swiftCrashSentence = "no index store described it, so the rules that read what its names resolve to " +
	"could not judge it (cohere-swift/correctness-no-unused-declaration); every other rule ran on it"

// goCrashSentence is cohere-go's crash, worded as internal/go_language/check's crashSentence words one: the
// rule, and what failed. Every other rule ran on the package.
const goCrashSentence = "cohere-go/correctness-no-ignored-error: analysis failed: unexpected node"

var crashLanguages = []crashLanguage{
	{
		name:        "TypeScript",
		render:      renderTypeScriptCrashes,
		directory:   "/project/source",
		extension:   ".ts",
		ruleCrashes: true,
		cause:       "index out of range",
		wantMessage: "cohere crashed on this file (index out of range). This is a bug in cohere, not in your code: " +
			"nothing in this file was checked. Please report it at " +
			reportBugsAt +
			" with the output of `cohere --version`, the rule, " +
			"the code that crashed it, and this message.",
	},
	{
		name:        "Swift",
		render:      renderSwiftCrashes,
		directory:   "/project/Sources",
		extension:   ".swift",
		language:    "Swift",
		cause:       swiftCrashSentence,
		wantMessage: "cohere-swift could not finish this file: " + swiftCrashSentence,
	},
	{
		name: "Go", render: renderGoCrashes, directory: "/project/internal", extension: ".go", language: "Go",
		cause:       goCrashSentence,
		wantMessage: "cohere-go could not finish this file: " + goCrashSentence,
	},
}

// renderTypeScriptCrashes runs the crashes through writeLintReport, as a walk's coverage names them.
func renderTypeScriptCrashes(test *testing.T, crashes []runCrash) string {
	test.Helper()
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"quiet-rule": 20},
		RulesListening: map[string]int{"quiet-rule": 20},
	}
	for _, crash := range crashes {
		if crash.Rule == "" {
			coverage.FilesCrashed = append(
				coverage.FilesCrashed,
				program.FileCrash{FileName: crash.Path, Cause: errors.New(crash.Cause)},
			)
		} else {
			coverage.RulesCrashed = append(
				coverage.RulesCrashed,
				program.RuleCrash{RuleName: crash.Rule, FileName: crash.Path, Cause: errors.New(crash.Cause)},
			)
		}
	}
	return renderLintReport(lintReport{
		Result:   program.Result{Coverage: coverage},
		Rules:    []rule.Entry{{Traits: rule.Traits{Name: "quiet-rule"}}},
		WalkCost: "in 1s",
	})
}

// renderSwiftCrashes runs the crashes through the Swift record stream.
func renderSwiftCrashes(test *testing.T, crashes []runCrash) string {
	test.Helper()
	return renderContractCrashes(test, swiftLanguage, crashes)
}

// renderGoCrashes runs the crashes through the same records in cohere-go's words, since every language that
// speaks the contract reports a crash the same way.
func renderGoCrashes(test *testing.T, crashes []runCrash) string {
	test.Helper()
	return renderContractCrashes(test, goLanguage("module"), crashes)
}

// renderContractCrashes runs the crashes through a contract child's record stream, in language's words: the
// contract's lint-only fixture with its lint record's crashes filled in, and its summary saying, as it must
// then, that the run is incomplete.
func renderContractCrashes(test *testing.T, language contractLanguage, crashes []runCrash) string {
	test.Helper()
	records := make([]map[string]string, 0, len(crashes))
	for _, crash := range crashes {
		records = append(records, map[string]string{"file": crash.Path, "error": crash.Cause})
	}
	encoded, encodeError := json.Marshal(records)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	stream := contractFixture(test, "LintOnly.jsonl")
	for index, line := range stream {
		line = strings.Replace(line, `"crashes":[]`, `"crashes":`+string(encoded), 1)
		stream[index] = strings.Replace(line, `"complete":true`, `"complete":false`, 1)
	}
	output, exitCode, renderRecordsInError := renderRecordsIn(test, language, contractModeCheck, stream, 1, nil)
	if renderRecordsInError != nil || exitCode != 1 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsInError, output)
	}
	return output
}

// renderCrashesIn renders through the child in the view asked for, from an empty summary, and returns what it
// printed and the summary it left. Both package-level settings are restored after.
func renderCrashesIn(
	test *testing.T,
	mode outputMode,
	language crashLanguage,
	crashes []runCrash,
) (string, runSummary) {
	test.Helper()
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	output := language.render(test, crashes)
	return output, activeSummary
}

// crashedFilesOf is count whole files lost to one panic, named F01, F02 and on, so they sort as numbered.
func crashedFilesOf(language crashLanguage, prefix string, count int, cause string) []runCrash {
	crashes := make([]runCrash, 0, count)
	for index := range count {
		crashes = append(crashes, runCrash{
			Path:     fmt.Sprintf("%s/%s%02d%s", language.directory, prefix, index+1, language.extension),
			Cause:    cause,
			Language: language.language,
		})
	}
	return crashes
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestADefaultRunNamesEachCrashGroupedByCause(test *testing.T) {
	for _, language := range crashLanguages {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		test.Run(language.name+": one crash names its file and its cause", func(test *testing.T) {
			crash := runCrash{
				Path:     language.directory + "/Broken" + language.extension,
				Cause:    language.cause,
				Language: language.language,
			}
			output, _ := renderCrashesIn(test, outputHuman, language, []runCrash{crash})
			want := "  crashed: " + crash.Path + ": " + language.wantMessage + "\n"
			if strings.Count(output, want) != 1 {
				test.Errorf("the default view did not name the crash once as\n%s\nin:\n%s", want, output)
			}
			forbidLines(test, output, "files, each:", "cohere --verbose names them all")
		})

		// Not parallel: it swaps the package-level activeOutput and activeSummary
		test.Run(language.name+
			": twelve of one cause name it once, five files, and count the rest", func(test *testing.T) {
			shared := crashedFilesOf(language, "F", 12, "index out of range")
			apart := runCrash{
				Path:     language.directory + "/Other" + language.extension,
				Cause:    "bad cast",
				Language: language.language,
			}
			output, _ := renderCrashesIn(test, outputHuman, language, append(append([]runCrash(nil), shared...), apart))

			sharedMessage := shared[0].message()
			block := "  crashed: 12 files, each: " + sharedMessage + "\n"
			for _, crash := range shared[:5] {
				block += "    " + crash.Path + "\n"
			}
			block += "    and 7 more; cohere --verbose names them all\n" +
				"  crashed: " + apart.Path + ": " + apart.message() + "\n"
			if !strings.Contains(output, block) {
				test.Errorf("the default view did not group the crashes as\n%s\nin:\n%s", block, output)
			}
			if count := strings.Count(output, sharedMessage); count != 1 {
				test.Errorf("the shared cause printed %d times, want once:\n%s", count, output)
			}
			for _, crash := range shared[5:] {
				forbidLines(test, output, crash.Path)
			}
			if language.name == "TypeScript" && output != block {
				// A TypeScript lint report prints nothing else in the default view, so the block is the whole of it.
				test.Errorf("the default view printed more than the crashes:\n%s", output)
			}
		})

		if !language.ruleCrashes {
			continue
		}
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		test.Run(language.name+
			": a rule's crash groups apart from the same panic losing a whole file", func(test *testing.T) {
			whole := crashedFilesOf(language, "W", 2, "nil pointer")
			ruled := []runCrash{
				{
					Path:  language.directory + "/R01" + language.extension,
					Rule:  "prefer-arrow-callback",
					Cause: "nil pointer",
				},
				{
					Path:  language.directory + "/R02" + language.extension,
					Rule:  "prefer-arrow-callback",
					Cause: "nil pointer",
				},
			}
			output, _ := renderCrashesIn(
				test,
				outputHuman,
				language,
				append(append([]runCrash(nil), whole...), ruled...),
			)
			requireLines(test, output,
				"  crashed: 2 files, each: "+whole[0].message()+"\n    "+whole[0].Path+"\n    "+whole[1].Path+"\n",
				"  crashed: 2 files, each: "+ruled[0].message()+"\n    "+ruled[0].Path+"\n    "+ruled[1].Path+"\n",
			)
			if !strings.Contains(ruled[0].message(), "cohere's rule prefer-arrow-callback crashed") {
				test.Errorf("a rule crash's cause does not name its rule: %s", ruled[0].message())
			}
			forbidLines(test, output, "more; cohere --verbose")
		})
	}
}

// A Swift crash is never a panic: the child reports a file its typed rules had no index for, and its sentence
// already says what that cost and that every other rule ran. So in both views it reads as the child said it,
// and never as cohere's crash, which would claim nothing in the file was checked and call it a bug to report.
// cohere-go's crash is a rule that failed on a package while the others ran, and reads the same way. A
// TypeScript crash is a panic, and keeps cohere's words.
// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestASwiftCrashSaysCohereSwiftsWordsInEveryView(test *testing.T) {
	cohereWords := []string{"bug in cohere", "nothing in this file was checked", reportBugsAt}
	for _, language := range crashLanguages {
		for _, mode := range []outputMode{outputHuman, outputVerbose} {
			view := map[outputMode]string{outputHuman: "default", outputVerbose: "verbose"}[mode]
			// Not parallel: it swaps the package-level activeOutput and activeSummary
			test.Run(language.name+" "+view, func(test *testing.T) {
				crash := runCrash{
					Path:     language.directory + "/Broken" + language.extension,
					Cause:    language.cause,
					Language: language.language,
				}
				output, _ := renderCrashesIn(test, mode, language, []runCrash{crash})
				requireLines(test, output, "  crashed: "+crash.Path+": "+language.wantMessage+"\n")
				if language.language != "" {
					forbidLines(test, output, cohereWords...)
				} else {
					requireLines(test, output, cohereWords...)
				}
			})
		}
	}
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestVerboseNamesEveryCrashOnceAndNoGroup(test *testing.T) {
	for _, language := range crashLanguages {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		test.Run(language.name, func(test *testing.T) {
			crashes := crashedFilesOf(language, "F", 12, "index out of range")
			output, _ := renderCrashesIn(test, outputVerbose, language, crashes)
			for _, crash := range crashes {
				line := "  crashed: " + crash.Path + ": " + crash.message() + "\n"
				if count := strings.Count(output, line); count != 1 {
					test.Errorf("--verbose named %s %d times, want once:\n%s", crash.Path, count, output)
				}
			}
			forbidLines(test, output, "files, each:", "cohere --verbose names them all")
		})
	}
}

// Not parallel: its subtests swap the package-level activeOutput and activeSummary
func TestJsonListsEveryCrashBesideTheCounts(test *testing.T) {
	for _, language := range crashLanguages {
		// Not parallel: it swaps the package-level activeOutput and activeSummary
		test.Run(language.name, func(test *testing.T) {
			crashes := append(
				crashedFilesOf(language, "F", 7, "index out of range"),
				crashedFilesOf(language, "G", 1, "bad cast")...,
			)
			if language.ruleCrashes {
				crashes = append(crashes, runCrash{
					Path:  language.directory + "/R01" + language.extension,
					Rule:  "prefer-arrow-callback",
					Cause: "nil pointer",
				})
			}
			output, summary := renderCrashesIn(test, outputJson, language, crashes)
			forbidLines(test, output, "crashed: ")

			encoded, encodeError := json.Marshal(summaryAsJson(summary))
			if encodeError != nil {
				test.Fatal(encodeError)
			}
			var decoded struct {
				Gaps struct {
					CrashedFiles int                 `json:"crashedFiles"`
					RuleCrashes  int                 `json:"ruleCrashes"`
					Crashes      []map[string]string `json:"crashes"`
				} `json:"gaps"`
			}
			if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
				test.Fatal(decodeError)
			}
			if len(decoded.Gaps.Crashes) != decoded.Gaps.CrashedFiles+decoded.Gaps.RuleCrashes {
				test.Errorf("--json lists %d crashes and counts %d crashed files and %d rule crashes:\n%s",
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
				test.Errorf("--json lists the crashes as %v, want %v:\n%s", got, want, encoded)
			}
		})
	}
}
