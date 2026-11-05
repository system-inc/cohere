package main

import (
	"bufio"
	"fmt"
	"sort"

	"github.com/system-inc/cohere/internal/types/program"
)

// reportBugsAt is where a user reports a bug in cohere. The README's "Reporting bugs" names the same
// place, and the two change together.
const reportBugsAt = "https://github.com/system-inc/cohere/issues"

// runCrash is a rule that could not finish a file, or a file nothing could check, because cohere crashed.
//
// A crash is cohere's bug, never the user's, and a verdict it leaves missing fails the run (#v1ah2qq).
// It used to be named only under --verbose while the run ended green: TanStack/query's six react-hooks
// crashes printed a footer marker and nothing else, so a missing verdict read like a clean one. The README
// already promised that a run which could not check a file never exits 0.
type runCrash struct {
	Path string `json:"path"`
	// Rule is the rule that crashed, empty when the whole file was lost.
	Rule  string `json:"rule,omitempty"`
	Cause string `json:"cause"`
	// Engine names the contract engine that could not finish the file, as a reader says it ("Swift", "Go"),
	// and is empty for a TypeScript crash. Such a crash's Cause is the engine's own sentence, and it is never a
	// panic of cohere's: the Swift engine reports a file its typed rules had no index for, the Go engine a rule
	// that failed on a package, and each says which rule it cost while every other rule ran. Wrapping that in
	// cohere's crash words would say nothing was checked and call it a bug to report, so message says the
	// engine's sentence instead. Kept out of `--json`, where the cause already is that sentence.
	Engine string `json:"-"`
}

// crashesFrom is a walk's crashes, whole files first, each group in the order the coverage names them.
func crashesFrom(coverage program.Coverage) []runCrash {
	crashes := make([]runCrash, 0, len(coverage.FilesCrashed)+len(coverage.RulesCrashed))
	for _, crash := range coverage.FilesCrashed {
		crashes = append(crashes, runCrash{Path: crash.FileName, Cause: fmt.Sprint(crash.Cause)})
	}
	for _, crash := range coverage.RulesCrashed {
		crashes = append(crashes, runCrash{Path: crash.FileName, Rule: crash.RuleName, Cause: fmt.Sprint(crash.Cause)})
	}
	return crashes
}

// message says what crashed, that it is cohere's bug, what is missing because of it, and where to report it.
// For a file a contract engine could not finish, it says the engine's sentence, which already names what is
// missing.
func (crash runCrash) message() string {
	if crash.Engine != "" {
		return "the " + crash.Engine + " engine could not finish this file: " + crash.Cause
	}
	what := fmt.Sprintf("cohere's rule %s crashed on this file (%s). This is a bug in cohere, not in your code: "+
		"the rule's verdict on this file is missing, and the file's other rules ran.", crash.Rule, crash.Cause)
	if crash.Rule == "" {
		what = fmt.Sprintf("cohere crashed on this file (%s). This is a bug in cohere, not in your code: "+
			"nothing in this file was checked.", crash.Cause)
	}
	return what + fmt.Sprintf(" Please report it at %s with the output of `cohere --version`, the rule, "+
		"the code that crashed it, and this message.", reportBugsAt)
}

// printCrash prints one crash as a line of JSON under `--json`. The human view names crashes grouped by what
// crashed (printCrashGroups), and `--verbose` names each in its coverage notes, in the same words.
func printCrash(out *bufio.Writer, crash runCrash) {
	if activeOutput.Mode == outputJSON {
		writeJSONLine(out, crashJSON{Kind: "crash", runCrash: crash})
	}
}

// printCrashGroups names a run's crashes in the human view, for every engine. `--verbose` names each crash
// on its own line in its coverage notes, and printing these as well would say every crash twice; `--json`
// lists them in the summary's gaps.
func printCrashGroups(out *bufio.Writer, crashes []runCrash) {
	if activeOutput.Mode == outputHuman {
		writeCrashGroups(out, crashes)
	}
}

// crashedNamedLimit is how many files of one cause the human view names before it counts the rest.
const crashedNamedLimit = 5

// writeCrashGroups says each distinct crash message once, with the files it cost under it:
//
//	crashed: 12 files, each: cohere crashed on this file (boom). This is a bug in cohere, ...
//	  Sources/A.swift
//	  Sources/B.swift
//	  Sources/C.swift
//	  Sources/D.swift
//	  Sources/E.swift
//	  and 7 more; cohere --verbose names them all
//	crashed: Sources/Z.swift: cohere crashed on this file (bad cast). This is a bug in cohere, ...
//
// Grouped, because a crash is almost always one defect met many times: a Swift lint-only run lost 1,251
// files to one cause, and a line per file buries the one sentence a reader must act on. A few files are
// named so the defect can be reproduced, and the rest counted. The cause is the message itself, so a rule
// crash groups apart from the same panic in another rule, or in a whole file. Largest group first.
func writeCrashGroups(out *bufio.Writer, crashes []runCrash) {
	type crashGroup struct {
		message string
		crashes []runCrash
	}
	var groups []*crashGroup
	byMessage := map[string]*crashGroup{}
	for _, crash := range crashes {
		message := crash.message()
		group, seen := byMessage[message]
		if !seen {
			group = &crashGroup{message: message}
			byMessage[message] = group
			groups = append(groups, group)
		}
		group.crashes = append(group.crashes, crash)
	}
	sort.SliceStable(groups, func(first, second int) bool {
		if len(groups[first].crashes) != len(groups[second].crashes) {
			return len(groups[first].crashes) > len(groups[second].crashes)
		}
		return groups[first].message < groups[second].message
	})
	for _, group := range groups {
		sort.Slice(
			group.crashes,
			func(first, second int) bool { return group.crashes[first].Path < group.crashes[second].Path },
		)
		if len(group.crashes) == 1 {
			writeCrashedNote(out, group.crashes[0])
			continue
		}
		fmt.Fprintf(out, "  crashed: %d files, each: %s\n", len(group.crashes), group.message)
		for _, crash := range group.crashes[:min(len(group.crashes), crashedNamedLimit)] {
			fmt.Fprintf(out, "    %s\n", crash.Path)
		}
		if rest := len(group.crashes) - crashedNamedLimit; rest > 0 {
			fmt.Fprintf(out, "    and %d more; cohere --verbose names them all\n", rest)
		}
	}
}

// crashJSON is one crash's line under `--json`.
type crashJSON struct {
	Kind string `json:"kind"`
	runCrash
}
