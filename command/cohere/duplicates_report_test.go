package main

import (
	"bufio"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// pinnedDuplicatesReport is one report with something in every part: a group of two with a kept member, a group of
// three, refusals of two reasons, functions never read, a withheld group, a dead suppression and a crash.
func pinnedDuplicatesReport() duplicate_functions.Report {
	return duplicate_functions.Report{
		Ran: true, Floor: 11, Files: 4, FilesIgnored: 1, Functions: 9, Hashed: 5,
		Groups: []duplicate_functions.Group{
			{Digest: duplicate_functions.Digest{1}, Size: 136, Members: []duplicate_functions.Function{
				{File: "/repo/a.ts", Line: 12, Name: "decompressArchive", Size: 136},
				{File: "/repo/b.ts", Line: 30, Name: "decompressArchive", Size: 136, Kept: "port of upstream"},
			}},
			{Digest: duplicate_functions.Digest{2}, Size: 11, Members: []duplicate_functions.Function{
				{File: "/repo/c.ts", Line: 1, Name: "hasDecorator", Size: 11},
				{File: "/repo/c.ts", Line: 9, Name: "hasDecoratorNamed", Size: 11},
				{File: "/repo/d.ts", Line: 4, Name: "Rule.hasDecorator", Size: 11},
			}},
		},
		Withheld: 1, BelowFloor: 7,
		Refused: []duplicate_functions.Refusal{
			{File: "/repo/a.ts", Line: 40, Name: "load", Reason: duplicate_functions.RefusalDynamicImport},
			{File: "/repo/d.ts", Line: 2, Name: "holder", Reason: duplicate_functions.RefusalUnsupportedNode,
				Detail: "KindClassDeclaration"},
		},
		NotRead:             map[duplicate_functions.NotReadReason]int{duplicate_functions.NotReadBodyless: 2},
		DeadSuppressions:    []duplicate_functions.Place{{File: "/repo/c.ts", Line: 20}},
		Crashes:             []duplicate_functions.Crash{{File: "/repo/e.ts", Error: "a panic"}},
		ElapsedMilliseconds: 410,
		Cache:               duplicate_functions.CacheUse{Unit: "files", Read: 3, Hashed: 1},
	}
}

// renderDuplicates prints a report in one view.
func renderDuplicates(test *testing.T, mode outputMode, report duplicate_functions.Report) string {
	test.Helper()
	saved := activeOutput
	defer func() { activeOutput = saved }()
	activeOutput = outputSettings{Mode: mode}
	var out strings.Builder
	stream := bufio.NewWriter(&out)
	writeDuplicatesReport(stream, report)
	flushStream(test, stream)
	return out.String()
}

const pinnedDuplicatesHuman = `
duplicates: a report, not a gate, nothing here fails a build

  functions that compute the same thing, size 11 and up: 2 groups, 5 functions
    size 136, one idea written twice
      /repo/a.ts:12 decompressArchive
      /repo/b.ts:30 decompressArchive, kept: port of upstream
    size 11, one idea written 3 times
      /repo/c.ts:1 hasDecorator
      /repo/c.ts:9 hasDecoratorNamed
      /repo/d.ts:4 Rule.hasDecorator
  looked at 9 functions in 4 files: hashed 5, refused 2, not read 2
  left out: 1 file the settings ignore
  refused, by reason: import() 1, syntax lowering does not model 1
  not read, by reason: no body 2
  withheld, every member kept by a suppression: 1 group
  suppressions that keep nothing: 1
    /repo/c.ts:20
  could not read /repo/e.ts: a panic
`

const pinnedDuplicatesVerbose = pinnedDuplicatesHuman + `  refused functions:
    import(): 1
      /repo/a.ts:40 load
    syntax lowering does not model: 1
      /repo/d.ts:2 holder (KindClassDeclaration)
  groups under size 11, not listed: 7
  grouped in 410ms, 3 files read from the cache and 1 hashed
`

const pinnedDuplicatesJson = `{"kind":"duplicate","size":136,"digest":"01000000000000000000000000000000","members":[{"path":"/repo/a.ts","line":12,"name":"decompressArchive"},{"path":"/repo/b.ts","line":30,"name":"decompressArchive","kept":"port of upstream"}]}
{"kind":"duplicate","size":11,"digest":"02000000000000000000000000000000","members":[{"path":"/repo/c.ts","line":1,"name":"hasDecorator"},{"path":"/repo/c.ts","line":9,"name":"hasDecoratorNamed"},{"path":"/repo/d.ts","line":4,"name":"Rule.hasDecorator"}]}
{"kind":"duplicateRefused","path":"/repo/a.ts","line":40,"name":"load","reason":"dynamicImport"}
{"kind":"duplicateRefused","path":"/repo/d.ts","line":2,"name":"holder","reason":"unsupportedNode","detail":"KindClassDeclaration"}
{"kind":"duplicateCoverage","ran":true,"floor":11,"files":4,"filesIgnored":1,"functions":9,"hashed":5,"refused":{"dynamicImport":1,"unsupportedNode":1},"notRead":{"bodyless":2},"groups":2,"withheld":1,"belowFloor":7,"deadSuppressions":[{"path":"/repo/c.ts","line":20}],"crashes":[{"path":"/repo/e.ts","error":"a panic"}],"seconds":0.41,"cache":{"unit":"files","read":3,"hashed":1}}
`

// The report's three views, pinned byte for byte.
// Not parallel: it sets the package-level activeOutput, which the printers' parallel tests read
func TestTheDuplicatesReportIsPinnedInEveryView(test *testing.T) {
	report := pinnedDuplicatesReport()
	for _, view := range []struct {
		name     string
		mode     outputMode
		expected string
	}{
		{"the human view", outputHuman, pinnedDuplicatesHuman},
		{"--verbose", outputVerbose, pinnedDuplicatesVerbose},
		{"--json", outputJson, pinnedDuplicatesJson},
	} {
		if rendered := renderDuplicates(test, view.mode, report); rendered != view.expected {
			test.Errorf("%s printed:\n%s\nwhere this was pinned:\n%s", view.name, rendered, view.expected)
		}
	}
}

// A report asked for that could not run says why in every view, and under --json still writes its coverage line, so
// its absence is never read as no duplicates.
// Not parallel: it sets the package-level activeOutput, which the printers' parallel tests read
func TestADuplicatesReportThatDidNotRunSaysWhy(test *testing.T) {
	report := duplicate_functions.Report{Floor: 11, NotRun: "types bailed with 1 type diagnostics"}
	human := renderDuplicates(test, outputHuman, report)
	if human != "\nduplicates: a report, not a gate, nothing here fails a build\n  not run: types bailed with 1 type "+
		"diagnostics\n" {
		test.Errorf("the human view printed:\n%s", human)
	}
	json := renderDuplicates(test, outputJson, report)
	if !strings.HasPrefix(
		json,
		`{"kind":"duplicateCoverage","ran":false,"notRun":"types bailed with 1 type diagnostics"`,
	) {
		test.Errorf("--json printed:\n%s", json)
	}
}
