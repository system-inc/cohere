package main

import (
	"errors"
	"fmt"
	"go/token"
	"io"
	"math/bits"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/go-types/types"
)

// runSpike lists every build, checks ours once for all of them and plainly for the baselines, and proves the
// projection to each build against that build's oracle.
func runSpike(asked commandLine, progress io.Writer) (*spikeReport, error) {
	root, absoluteError := filepath.Abs(asked.root)
	if absoluteError != nil {
		return nil, absoluteError
	}
	side, sideError := sideWorkFile(root)
	if sideError != nil {
		return nil, fmt.Errorf("copying the go.work aside: %w", sideError)
	}
	defer side.remove()
	configurations := append([]configuration{asked.host}, asked.builds...)
	architecture, architectureError := sameArchitecture(configurations)
	if architectureError != nil {
		return nil, architectureError
	}
	report := &spikeReport{
		Root:      root,
		Patterns:  asked.patterns,
		Workspace: "GOWORK=off, the tree holds no go.work",
		Notes:     spikeNotes,
	}
	if side.directory != "" {
		report.Workspace = "a side copy of the tree's go.work, every directory made absolute"
	}

	listed := make([]*listedBuild, 0, len(configurations))
	var listError error
	report.Timing.ListingGraph = measure(func() {
		for _, build := range configurations {
			say(progress, "variational-spike: listing %s\n", build.target(build.cgo == "1"))
			each, eachError := listBuild(root, asked.patterns, build, side.workFile)
			if eachError != nil {
				listError = eachError
				return
			}
			listed = append(listed, each)
		}
	})
	if listError != nil {
		return nil, listError
	}
	for index, each := range listed {
		report.Builds = append(report.Builds, buildListing{
			Bit:        index,
			Target:     each.configuration.target(each.cgoEnabled),
			CgoEnabled: each.cgoEnabled,
			CgoChoice:  each.configuration.cgoChoice(),
			Packages:   len(each.packages),
			Matched:    len(each.matched),
		})
	}
	ours, oursError := collectOurs(listed)
	if oursError != nil {
		return nil, oursError
	}
	if len(ours) == 0 {
		return nil, errors.New("the patterns match no package in any build")
	}
	report.Timing.ListingExports = measure(func() {
		for _, each := range listed {
			say(progress, "variational-spike: listing export data for %s\n", each.configuration.target(each.cgoEnabled))
			if exportError := listExports(root, each, side.workFile, ours); exportError != nil {
				listError = exportError
				return
			}
		}
	})
	if listError != nil {
		return nil, listError
	}
	for index, each := range listed {
		report.Builds[index].Exported = len(each.exports())
	}
	fileSet := token.NewFileSet()
	var parseError error
	report.Timing.Parse = measure(func() { parseError = parseOurs(fileSet, ours, listed) })
	if parseError != nil {
		return nil, parseError
	}
	ordered, orderError := dependencyOrder(ours)
	if orderError != nil {
		return nil, orderError
	}

	all := types.Mask(1)<<len(listed) - 1
	sizes := types.SizesFor("gc", architecture)
	exports := make([]map[string]string, len(listed))
	for index, each := range listed {
		exports[index] = each.exports()
	}
	importer := merged.New(fileSet, exportSources(fileSet, exports), all)

	say(progress, "variational-spike: checking %d packages once for %d builds\n", len(ordered), len(listed))
	checked, variationalCounts := checkVariationally(fileSet, ordered, ours, importer, all, sizes, measure)
	report.Timing.VariationalImport = variationalCounts.imports
	report.Timing.VariationalCheck = variationalCounts.checks
	importerTiming := importer.Timing()
	report.Timing.VariationalDecodeSeconds = importerTiming.Decode.Seconds()
	report.Timing.VariationalDecodeWaitSeconds = importerTiming.DecodeWait.Seconds()
	report.Timing.VariationalMergeSeconds = importerTiming.Merge.Seconds()
	report.Errors.Variational = variationalCounts.errors
	report.Errors.VariationalFirst = variationalCounts.firstErrors
	// A build's export data the merge couldn't read is an error of the variational check, for that build.
	for _, failure := range importer.Failures() {
		report.Errors.Variational++
		if len(report.Errors.VariationalFirst) < firstErrorsKept {
			report.Errors.VariationalFirst = append(report.Errors.VariationalFirst, failure.Error())
		}
	}
	report.addOurs(checked)

	say(progress, "variational-spike: checking every build plainly\n")
	// A fresh importer: the merge's shares each build's decoded packages with its Plain, so reusing it would hide
	// the baselines' decode in the variational run's.
	baselineImporter := merged.New(fileSet, exportSources(fileSet, exports), all)
	for index, each := range listed {
		counts := checkPlainly(fileSet, ordered, each, baselineImporter.Plain(index), index, sizes, measure)
		if index == 0 {
			report.Timing.HostPlainImport = counts.imports
			report.Timing.HostPlainCheck = counts.checks
			report.Timing.HostPlainDecodeSeconds = baselineImporter.Timing().Decode.Seconds()
		}
		report.Timing.EveryBuildPlainImport = report.Timing.EveryBuildPlainImport.add(counts.imports)
		report.Timing.EveryBuildPlainCheck = report.Timing.EveryBuildPlainCheck.add(counts.checks)
		report.Timing.PlainCheckByBuild = append(report.Timing.PlainCheckByBuild, counts.checks)
		report.Errors.Plain += counts.errors
		for _, spelled := range counts.firstErrors {
			if len(report.Errors.PlainFirst) < firstErrorsKept {
				report.Errors.PlainFirst = append(report.Errors.PlainFirst, report.Builds[index].Target+": "+spelled)
			}
		}
	}
	report.Timing.PlainDecodeSeconds = baselineImporter.Timing().Decode.Seconds()
	report.Shortfalls = importer.Shortfalls()

	if asked.cpuProfile != "" {
		say(progress, "variational-spike: profiling each check %d times\n", asked.profileRepeat)
		profiled, profileError := profileChecks(fileSet, ordered, ours, importer, all, listed[0],
			baselineImporter.Plain(0), sizes, asked.profileRepeat, asked.cpuProfile)
		if profileError != nil {
			return nil, profileError
		}
		report.Profile = profiled
	}

	sources := &sourceCache{contents: map[string][]byte{}}
	for index, each := range listed {
		say(progress, "variational-spike: proving %s against its oracle\n", report.Builds[index].Target)
		oracle, oracleError := loadOracle(root, asked.patterns, each.configuration, side.workFile, ours)
		if oracleError != nil {
			return nil, oracleError
		}
		projected := projectTo(fileSet, checked, index)
		if asked.plant && index == 0 {
			planted := plantMismatch(projected)
			report.Planted = &differenceAtKey{Build: index, File: planted.file, Start: planted.start, End: planted.end}
		}
		report.Proof = append(report.Proof, proveBuild(report.Builds[index].Target, projected, oracle, sources))
		// The build's whole graph is garbage now, and the next build's load needs the room.
		oracle = nil
		runtime.GC()
		debug.FreeOSMemory()
	}
	report.PeakResidentBytes = peakResidentBytes(runtime.GOOS)
	return report, nil
}

// exportSources is a source for each build, reading the export data files exports[b] names for build b.
func exportSources(fileSet *token.FileSet, exports []map[string]string) []merged.Source {
	sources := make([]merged.Source, len(exports))
	for index, files := range exports {
		sources[index] = merged.ExportData(fileSet, files)
	}
	return sources
}

// addOurs reports each of checked and sums them.
//
// Mutates: report gets ours and the totals.
func (report *spikeReport) addOurs(checked []*checkedPackage) {
	for _, each := range checked {
		inEvery := 0
		for _, file := range each.our.files {
			if each.our.masks[file] == each.variation.Builds {
				inEvery++
			}
		}
		forked := 0
		for _, answers := range each.variation.Types {
			if answers.Len() > 1 {
				forked++
			}
		}
		our := ourReport{
			Path:              each.our.path,
			Files:             len(each.our.files),
			FilesInEveryBuild: inEvery,
			Builds:            bits.OnesCount8(uint8(each.our.builds)),
			Units:             each.variation.Units,
			Restarts:          each.variation.Restarts,
			Expressions:       len(each.variation.Types),
			ForkedExpressions: forked,
		}
		report.Ours = append(report.Ours, our)
		report.Totals.Packages++
		report.Totals.Files += our.Files
		report.Totals.FilesInEveryBuild += our.FilesInEveryBuild
		report.Totals.Units += our.Units
		report.Totals.Restarts += our.Restarts
		report.Totals.Expressions += our.Expressions
		report.Totals.ForkedExpressions += our.ForkedExpressions
	}
}

// green says whether the run proved what it set out to: every build's projection matched its oracle exactly and no
// check reported an error.
func (report *spikeReport) green() bool {
	if report.Errors.Variational != 0 || report.Errors.Plain != 0 || len(report.Proof) == 0 {
		return false
	}
	for _, proof := range report.Proof {
		if !proof.Green {
			return false
		}
	}
	return true
}
