package go_engine

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/go_engine/canonical_function_hash"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// duplicatesRequested is `--duplicates`.
func (engine *pipeline) duplicatesRequested() bool {
	return engine.request.switches["duplicates"]
}

// duplicatesReport groups what the lint pass hashed into the duplicates report (#jfvhbj7), over the functions in the
// files in scope.
//
// Every function of a package is read again in its test variant, `p [p.test]`, so each is counted once, by its file,
// line and name; the two readings of one function agree, the type checker giving both the same types. A suppression
// naming duplicate_functions.GoRuleName keeps a function in its shape, and one that keeps no member of a group at the
// floor is listed as dead, never a finding: the report never moves the verdict.
func (engine *pipeline) duplicatesReport(result *check.Result) *duplicate_functions.Report {
	started := time.Now()
	report := &duplicate_functions.Report{
		Ran: true,
		Cache: duplicate_functions.CacheUse{Unit: "packages", Read: result.ReportsRestored,
			Hashed: len(result.Reports) - result.ReportsRestored},
	}
	type place struct {
		file string
		line int
		name string
	}
	seen := map[place]bool{}
	first := func(file string, line int, name string) bool {
		key := place{file, line, name}
		if seen[key] || !engine.described.inScopeSet[file] {
			return false
		}
		seen[key] = true
		return true
	}
	files := map[string]bool{}
	var functions []duplicate_functions.Function
	for _, analyzed := range sortedReports(result.Reports) {
		for _, function := range analyzed.Functions {
			if first(function.File, function.Line, function.Name) {
				functions = append(functions, function)
				files[function.File] = true
			}
		}
		for _, refused := range analyzed.Refused {
			if first(refused.File, refused.Line, refused.Name) {
				report.Refuse(refused)
				files[refused.File] = true
			}
		}
		for _, unread := range analyzed.Unread {
			if first(unread.File, unread.Line, unread.Name) {
				report.Skip(unread.Reason, 1)
				files[unread.File] = true
			}
		}
	}
	for _, crash := range result.ReportCrashes {
		report.Crashes = append(report.Crashes, duplicate_functions.Crash{File: crash.File, Error: crash.Error})
		engine.note("the duplicates report could not read %s: %s", crash.File, crash.Error)
	}
	report.Files = len(files)

	var keepings []suppression.Keeping
	for _, file := range engine.described.readable() {
		if engine.described.inScopeSet[file] {
			keepings = append(keepings, suppression.Build(file, engine.described.contents[file]).Keepings(
				duplicate_functions.GoRuleName)...)
		}
	}
	keeperOf := map[duplicate_functions.Place]int{}
	for index := range functions {
		if keeper, kept := keepingFor(keepings, functions[index].File, functions[index].Line); kept {
			functions[index].Kept = keepings[keeper].Reason
			keeperOf[duplicate_functions.Place{File: functions[index].File, Line: functions[index].Line}] = keeper
		}
	}
	atFloor := report.Assemble(functions)
	alive := map[int]bool{}
	for member := range atFloor {
		if keeper, kept := keeperOf[member]; kept {
			alive[keeper] = true
		}
	}
	for index, keeping := range keepings {
		if !alive[index] {
			report.DeadSuppressions = append(report.DeadSuppressions,
				duplicate_functions.Place{File: keeping.File, Line: keeping.Line})
		}
	}
	report.SortRefused()
	report.ElapsedMilliseconds = int(time.Since(started).Milliseconds())
	return report
}

// keepingFor is the directive that keeps the function at a file's line: one on its line before a file-ignore, as
// suppression credits a line's own directive first.
func keepingFor(keepings []suppression.Keeping, file string, line int) (int, bool) {
	for _, wholeFile := range []bool{false, true} {
		for index, keeping := range keepings {
			if keeping.File == file && keeping.WholeFile == wholeFile && keeping.Covers(line) {
				return index, true
			}
		}
	}
	return 0, false
}

// sortedReports are the hasher's results by package ID, so the reading kept of a function read twice is the same
// every run. A result of another type is the hasher's own defect, and is left out rather than read wrongly.
func sortedReports(reports map[*packages.Package]any) []*canonical_function_hash.PackageFunctions {
	analyzed := slices.SortedFunc(maps.Keys(reports), func(left *packages.Package, right *packages.Package) int {
		return cmp.Compare(left.ID, right.ID)
	})
	sorted := make([]*canonical_function_hash.PackageFunctions, 0, len(analyzed))
	for _, analyzedPackage := range analyzed {
		if functions, isFunctions := reports[analyzedPackage].(*canonical_function_hash.PackageFunctions); isFunctions {
			sorted = append(sorted, functions)
		}
	}
	return sorted
}

// writeDuplicates writes the duplicates record under `--duplicates`, after the unused phase's: what the lint phase
// grouped, or why it grouped nothing.
func (engine *pipeline) writeDuplicates() {
	if !engine.duplicatesRequested() {
		return
	}
	report := engine.duplicates
	if report == nil {
		notRun := engine.duplicatesNotRun
		if notRun == "" {
			notRun = "the lint phase never read the functions"
		}
		report = &duplicate_functions.Report{NotRun: notRun, Floor: duplicate_functions.Floor}
	}
	engine.write(contract.DuplicatesRecord{Report: *report})
}
