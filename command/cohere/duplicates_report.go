package main

import (
	"bufio"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/streams"
)

// The duplicates report (#jfvhbj7): functions whose canonical IR is one graph, grouped, at size 11 and up. A report,
// never a gate (@system_cohere's ruling on #6rj7jw7): it follows the phases rather than being one, so no phase line or
// engine contract changes, it is never a finding, and the run's exit code is the run's whichever groups it lists.
//
// TypeScript builds the report in this process (duplicates_typescript.go), and the Go engine sends it as one
// duplicates record (engine_run.go). Both print here, so a duplicate reads the same whichever engine found it.

// refusalPhrases are the refusal reasons in the report's words.
var refusalPhrases = map[duplicate_functions.RefusalReason]string{
	duplicate_functions.RefusalParameterDefault:       "a parameter default that is not a literal",
	duplicate_functions.RefusalObjectMethodCapture:    "an object method that captures",
	duplicate_functions.RefusalFieldInitializers:      "a constructor beside field initializers",
	duplicate_functions.RefusalUnresolvedCapture:      "a capture lowering could not resolve",
	duplicate_functions.RefusalDynamicImport:          "import()",
	duplicate_functions.RefusalUnsupportedNode:        "syntax lowering does not model",
	duplicate_functions.RefusalUnsupportedInstruction: "an instruction the hash does not model",
	duplicate_functions.RefusalNotHashable:            "an IR the hash could not walk",
}

// notReadPhrases are the reasons a function was never read, in the report's words.
var notReadPhrases = map[duplicate_functions.NotReadReason]string{
	duplicate_functions.NotReadBodyless:  "no body",
	duplicate_functions.NotReadGenerated: "in a generated file",
	duplicate_functions.NotReadCopied:    "in a file copied from upstream",
}

// writeDuplicatesReport prints the report in the view the run asked for: as JSON lines under `--json`, and otherwise
// as text, with every refused function and what the report cost added under `--verbose`.
func writeDuplicatesReport(out *bufio.Writer, report duplicate_functions.Report) {
	if activeOutput.Mode == outputJson {
		writeDuplicatesJson(out, report)
		streams.Show(out)
		return
	}
	fmt.Fprintf(out, "\nduplicates: a report, not a gate, nothing here fails a build\n")
	if !report.Ran {
		fmt.Fprintf(out, "  not run: %s\n", report.NotRun)
		streams.Show(out)
		return
	}
	writeDuplicateGroups(out, report)
	writeDuplicatesCoverage(out, report)
	if activeOutput.Mode == outputVerbose {
		writeDuplicatesAccount(out, report)
	}
	streams.Show(out)
}

// writeDuplicateGroups prints each group, largest first, every member as file:line and name.
func writeDuplicateGroups(out *bufio.Writer, report duplicate_functions.Report) {
	if len(report.Groups) == 0 {
		fmt.Fprintf(out, "\n  no two functions compute the same thing at size %d and up\n", report.Floor)
		return
	}
	functions := 0
	for _, group := range report.Groups {
		functions += len(group.Members)
	}
	fmt.Fprintf(out, "\n  functions that compute the same thing, size %d and up: %s, %s\n", report.Floor,
		counted(len(report.Groups), "group", "groups"), counted(functions, "function", "functions"))
	for _, group := range report.Groups {
		fmt.Fprintf(out, "    size %d, one idea written %s\n", group.Size, timesWritten(len(group.Members)))
		for _, member := range group.Members {
			kept := ""
			if member.Kept != "" {
				kept = ", kept: " + member.Kept
			}
			fmt.Fprintf(out, "      %s:%d %s%s\n", member.File, member.Line, member.Name, kept)
		}
	}
}

// writeDuplicatesCoverage prints what the report looked at beside what it found, so a report that read nothing never
// reads like one that found nothing.
func writeDuplicatesCoverage(out *bufio.Writer, report duplicate_functions.Report) {
	notRead := 0
	for _, count := range report.NotRead {
		notRead += count
	}
	fmt.Fprintf(out, "  looked at %s in %s: hashed %d, refused %d, not read %d\n",
		counted(report.Functions, "function", "functions"), counted(report.Files, "file", "files"), report.Hashed,
		len(report.Refused), notRead)
	if report.FilesIgnored > 0 {
		fmt.Fprintf(out, "  left out: %s the settings ignore\n", counted(report.FilesIgnored, "file", "files"))
	}
	if len(report.Refused) > 0 {
		refusedBy := map[string]int{}
		for _, refused := range report.Refused {
			refusedBy[refusalPhrase(refused.Reason)]++
		}
		fmt.Fprintf(out, "  refused, by reason: %s\n", byReason(refusedBy))
	}
	if notRead > 0 {
		notReadBy := map[string]int{}
		for reason, count := range report.NotRead {
			notReadBy[notReadPhrase(reason)] += count
		}
		fmt.Fprintf(out, "  not read, by reason: %s\n", byReason(notReadBy))
	}
	if report.Withheld > 0 {
		fmt.Fprintf(out, "  withheld, every member kept by a suppression: %s\n",
			counted(report.Withheld, "group", "groups"))
	}
	if len(report.DeadSuppressions) > 0 {
		fmt.Fprintf(out, "  suppressions that keep nothing: %d\n", len(report.DeadSuppressions))
		for _, dead := range report.DeadSuppressions {
			fmt.Fprintf(out, "    %s:%d\n", dead.File, dead.Line)
		}
	}
	for _, crash := range report.Crashes {
		fmt.Fprintf(out, "  could not read %s: %s\n", crash.File, crash.Error)
	}
}

// writeDuplicatesAccount is `--verbose`'s part: every refused function under its reason, the groups under the floor,
// and what the report cost.
func writeDuplicatesAccount(out *bufio.Writer, report duplicate_functions.Report) {
	if len(report.Refused) > 0 {
		byPhrase := map[string][]duplicate_functions.Refusal{}
		for _, refused := range report.Refused {
			phrase := refusalPhrase(refused.Reason)
			byPhrase[phrase] = append(byPhrase[phrase], refused)
		}
		fmt.Fprintf(out, "  refused functions:\n")
		for _, phrase := range slices.Sorted(maps.Keys(byPhrase)) {
			fmt.Fprintf(out, "    %s: %d\n", phrase, len(byPhrase[phrase]))
			for _, refused := range byPhrase[phrase] {
				detail := ""
				if refused.Detail != "" {
					detail = " (" + refused.Detail + ")"
				}
				fmt.Fprintf(out, "      %s:%d %s%s\n", refused.File, refused.Line, refused.Name, detail)
			}
		}
	}
	fmt.Fprintf(out, "  groups under size %d, not listed: %d\n", report.Floor, report.BelowFloor)
	fmt.Fprintf(out, "  grouped in %s, %d %s read from the cache and %d hashed\n",
		round(time.Duration(report.ElapsedMilliseconds)*time.Millisecond), report.Cache.Read, report.Cache.Unit,
		report.Cache.Hashed)
}

// timesWritten is how many times one idea was written: twice, or a count.
func timesWritten(members int) string {
	if members == 2 {
		return "twice"
	}
	return fmt.Sprintf("%d times", members)
}

// refusalPhrase is a refusal reason's words, or the reason itself for one this binary has no words for.
func refusalPhrase(reason duplicate_functions.RefusalReason) string {
	if phrase, known := refusalPhrases[reason]; known {
		return phrase
	}
	return string(reason)
}

// notReadPhrase is a not-read reason's words, or the reason itself for one this binary has no words for.
func notReadPhrase(reason duplicate_functions.NotReadReason) string {
	if phrase, known := notReadPhrases[reason]; known {
		return phrase
	}
	return string(reason)
}

// byReason is counts by reason, largest first, ties by reason, as `import() 905, no body 55`.
func byReason(counts map[string]int) string {
	reasons := slices.SortedFunc(maps.Keys(counts), func(left string, right string) int {
		if counts[left] != counts[right] {
			return counts[right] - counts[left]
		}
		return strings.Compare(left, right)
	})
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%s %d", reason, counts[reason]))
	}
	return strings.Join(parts, ", ")
}
