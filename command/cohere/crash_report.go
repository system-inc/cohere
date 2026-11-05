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

// printCrash prints one crash as a line of JSON under `--json`. The human view names crashes grouped by what
// crashed (printCrashGroups), and `--verbose` names each in its coverage notes, in the same words.
func printCrash(out *bufio.Writer, crash runCrash) {
	if activeOutput.Mode == outputJson {
		writeJsonLine(out, crashJson{Kind: "crash", runCrash: crash})
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

// crashJson is one crash's line under `--json`.
type crashJson struct {
	Kind string `json:"kind"`
	runCrash
}
