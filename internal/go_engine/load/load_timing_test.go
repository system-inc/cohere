package load

import (
	"fmt"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// timedModule is a module of two packages and three files that imports nothing outside it, so its load parses and
// type-checks those alone: a, of two files, imports b.
var timedModule = map[string]string{
	"go.mod": "module example.com/timed\n\ngo 1.24\n",
	"a/a.go": "package a\n\nimport \"example.com/timed/b\"\n\n// Twice is two of b's.\n" +
		"func Twice() int { return 2 * b.One() }\n",
	"a/more.go": "package a\n\n// Thrice is one more than Twice.\nfunc Thrice() int { return Twice() + 1 }\n",
	"b/b.go":    "package b\n\n// One is one.\nfunc One() int { return 1 }\n",
}

// timedFiles and timedPackages are how many files and packages timedModule's load parses and checks from source.
const (
	timedFiles    = 3
	timedPackages = 2
)

// timedLoad loads timedModule, written in a directory of its own, with collector, nil for a load that did not ask.
func timedLoad(test *testing.T, collector *timing.Collector) *Loaded {
	test.Helper()
	directory := test.TempDir()
	writeListingFiles(test, directory, timedModule)
	loaded, loadError := PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		Caching{Timing: collector})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	return loaded
}

// A load handed a collector adds its phases to it: each file it parsed once, each package it checked from source
// once, and what parsing, checking and listing cost, none of which is free.
func TestALoadAddsItsPhasesToTheCollector(test *testing.T) {
	test.Parallel()

	collector := timing.New()
	loaded := timedLoad(test, collector)
	record := collector.Record(func(string) (string, contract.SharedKind) { return "", contract.SharedAnalyzer })
	if record.Unavailable != "" {
		test.Skipf("no CPU to measure here: %s", record.Unavailable)
	}
	test.Logf("listed in process %t (%s), %+v", loaded.ListedInProcess, loaded.GoListReason, record.Load)
	if loaded.FilesParsed != timedFiles {
		test.Fatalf("the load parsed %d files, want %d, so the fixture is not the one the counts below are of",
			loaded.FilesParsed, timedFiles)
	}
	if record.Load.ParsedFiles != loaded.FilesParsed {
		test.Errorf("the record counts %d parsed files, and the load parsed %d", record.Load.ParsedFiles,
			loaded.FilesParsed)
	}
	if record.Load.TypeCheckedPackages != timedPackages {
		test.Errorf("the record counts %d packages checked from source, want %d",
			record.Load.TypeCheckedPackages, timedPackages)
	}
	if record.Load.ExportDataPackages != 0 || loaded.PackagesFromExportData != 0 {
		test.Errorf("the record counts %d packages read from export data and the load %d, with no cache to read",
			record.Load.ExportDataPackages, loaded.PackagesFromExportData)
	}
	for _, phase := range []struct {
		name string
		cpu  int64
	}{
		{"parse", record.Load.ParseCpuMicroseconds},
		{"type-check", record.Load.TypeCheckCpuMicroseconds},
		{"listing", record.Load.ListCpuMicroseconds},
		{"listing's go commands", record.Load.ListChildrenCpuMicroseconds},
	} {
		if phase.cpu <= 0 {
			test.Errorf("the %s cost %dµs, so it was not measured", phase.name, phase.cpu)
		}
	}
}

// A load without a collector loads what a load with one does: measuring changes nothing it returns.
func TestALoadWithoutACollectorLoadsTheSame(test *testing.T) {
	test.Parallel()

	measured := timedSummary(timedLoad(test, timing.New()))
	unmeasured := timedSummary(timedLoad(test, nil))
	if !slices.Equal(measured, unmeasured) {
		test.Errorf("without a collector the load gave\n%v\nand with one\n%v", unmeasured, measured)
	}
}

// timedSummary is what a load of timedModule says of its packages, their files, imports, errors and declarations, and
// of its counts, one line each and sorted, with the directory each load was written in left out, so two loads compare.
func timedSummary(loaded *Loaded) []string {
	summary := []string{fmt.Sprintf("files parsed %d, packages loaded %d, diagnostics %d", loaded.FilesParsed,
		loaded.PackagesLoaded, len(loaded.Diagnostics))}
	for _, loadedPackage := range loaded.Packages {
		imports := make([]string, 0, len(loadedPackage.Imports))
		for path := range loadedPackage.Imports {
			imports = append(imports, path)
		}
		slices.Sort(imports)
		declared := []string{}
		if loadedPackage.Types != nil {
			declared = loadedPackage.Types.Scope().Names()
		}
		summary = append(summary, fmt.Sprintf("%s: %d files, %d parsed, imports %v, errors %v, declares %v",
			loadedPackage.ID, len(loadedPackage.GoFiles), len(loadedPackage.Syntax), imports, loadedPackage.Errors,
			declared))
	}
	slices.Sort(summary)
	return summary
}
