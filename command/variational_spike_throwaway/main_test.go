package main

import (
	"go/token"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/go-types/types"
)

// fixtureRoot is the platform census's fork fixture: N, Kill and Stat declared once per platform, and functions that
// read them beside functions that don't.
var fixtureRoot = filepath.Join("..", "platform_census_throwaway", "testdata", "fork")

// fixtureCommandLine checks the fixture for darwin, linux and windows on arm64. The fixture requires no module and asks
// for this go, so its go list reaches no proxy and fetches no toolchain.
func fixtureCommandLine(test *testing.T, plant bool) commandLine {
	test.Helper()
	asked, parseError := parseCommandLine([]string{
		"--root", fixtureRoot,
		"--host", "darwin/arm64,cgo=0",
		"--build", "linux/arm64",
		"--build", "windows/arm64",
		"--patterns", "./...",
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	asked.plant = plant
	return asked
}

// TestFixtureProjectsExactlyToEveryBuild runs the fixture through the whole spike: one variational check, each build's
// projection compared with go/packages' plain answers, every expression matched and none on one side only.
func TestFixtureProjectsExactlyToEveryBuild(test *testing.T) {
	test.Parallel()
	report, spikeError := runSpike(fixtureCommandLine(test, false), io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if !report.green() {
		test.Errorf("the fixture's proof is red:\n%s", tableText(report))
	}
	if len(report.Proof) != 3 {
		test.Fatalf("%d builds proved, want 3", len(report.Proof))
	}
	for _, proof := range report.Proof {
		if proof.Compared == 0 || proof.Matched != proof.Compared {
			test.Errorf("%s: %d of %d matched", proof.Target, proof.Matched, proof.Compared)
		}
	}
	requireEveryMapGreen(test, report, mapExpectations{overlaid: true})
	if report.Totals.ForkedExpressions == 0 || report.Totals.Restarts == 0 {
		test.Errorf("the fixture forks, yet %d expressions have more than one answer and %d units restarted",
			report.Totals.ForkedExpressions, report.Totals.Restarts)
	}
}

// TestPlantedMismatchShowsRed changes one projected answer of the host before the proof, which must count exactly that
// one as mismatched and call the run red, while the other builds stay green.
func TestPlantedMismatchShowsRed(test *testing.T) {
	test.Parallel()
	report, spikeError := runSpike(fixtureCommandLine(test, true), io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if report.green() {
		test.Fatal("a planted mismatch left the run green")
	}
	host := report.Proof[0]
	if host.Green || host.Mismatched != 1 || len(host.FirstMismatches) != 1 {
		test.Fatalf("host: %d mismatched, %d mismatches spelled, want the one planted", host.Mismatched,
			len(host.FirstMismatches))
	}
	if !strings.HasSuffix(host.FirstMismatches[0].Projected, plantedSuffix) {
		test.Errorf("the mismatch spelled is %+v, not the planted one", host.FirstMismatches[0])
	}
	for _, proof := range report.Proof[1:] {
		if !proof.Green {
			test.Errorf("%s turned red, though nothing was planted there", proof.Target)
		}
	}
}

// TestMixedArchitecturesAreRefused refuses builds of two architectures, which one Sizes can't check.
func TestMixedArchitecturesAreRefused(test *testing.T) {
	test.Parallel()
	_, parseError := parseCommandLine([]string{"--root", ".", "--host", "darwin/arm64", "--build", "linux/amd64"},
		io.Discard)
	if parseError == nil || !strings.Contains(parseError.Error(), "mix architectures") {
		test.Fatalf("mixed architectures: %v, want a refusal", parseError)
	}
}

// parallelFixtureRoot is nine packages of ours: shape, imported by left and right, both imported by top; words and
// records beside them, importing nothing of ours; and platform and status, whose files differ by build (a handle, a
// separator and a count of another type in each, and syscall's Stat_t where the build has one), both imported with
// shape by uses. Every one reads the standard library's types, many of them the same ones, so checks that run at once
// read one importer at once.
var parallelFixtureRoot = filepath.Join("testdata", "parallel")

// parallelFixturePackages is how many of ours the parallel fixture holds.
const parallelFixturePackages = 9

// parallelWorkers is more workers than the fixture's widest level of the import graph, so every package that can run
// beside another does.
const parallelWorkers = 4

// TestParallelPlainChecksAreSafe lists the parallel fixture for one build and checks it plainly on the engine's
// schedule several times, each through a fresh whole-reading importer warmed first, as the parallel host baseline
// does. Under `-race` a check reading what another job writes fails the test; without it the checks must still report
// no error, time every package and decode nothing, the warm-up having read every package they import.
func TestParallelPlainChecksAreSafe(test *testing.T) {
	test.Parallel()
	root, rootError := filepath.Abs(parallelFixtureRoot)
	if rootError != nil {
		test.Fatal(rootError)
	}
	host, hostError := parseConfiguration("darwin/arm64,cgo=0")
	if hostError != nil {
		test.Fatal(hostError)
	}
	host.isHost = true
	listed, listError := listBuild(root, []string{"./..."}, host, "off", false)
	if listError != nil {
		test.Fatal(listError)
	}
	ours, oursError := collectOurs([]*listedBuild{listed})
	if oursError != nil {
		test.Fatal(oursError)
	}
	if exportError := listExports(root, listed, "off", ours); exportError != nil {
		test.Fatal(exportError)
	}
	fileSet := token.NewFileSet()
	if parseError := parseOurs(fileSet, ours, []*listedBuild{listed}); parseError != nil {
		test.Fatal(parseError)
	}
	ordered, orderError := dependencyOrder(ours)
	if orderError != nil {
		test.Fatal(orderError)
	}
	if len(ordered) != parallelFixturePackages {
		test.Fatalf("%d packages of ours, want %d", len(ordered), parallelFixturePackages)
	}
	sizes := types.SizesFor("gc", host.architecture)
	exports := []map[string]string{listed.exports()}
	for range 3 {
		importer := merged.New(fileSet, exportSources(fileSet, exports), 1)
		hostPlainWhole := importer.PlainWhole(0)
		warmPlainly(ordered, ours, listed, hostPlainWhole, 0)
		warmDecode := importer.Timing().Decode
		if warmDecode == 0 {
			test.Fatal("the warm-up decoded nothing")
		}
		counts, run := checkPlainlyInParallel(fileSet, ordered, ours, listed, hostPlainWhole, 0, sizes,
			parallelWorkers)
		if scheduleDecode := importer.Timing().Decode - warmDecode; scheduleDecode != 0 {
			test.Errorf("the schedule after the warm-up decoded for %s, want nothing", scheduleDecode)
		}
		if counts.errors != 0 {
			test.Fatalf("the parallel plain check reported %d errors: %q", counts.errors, counts.firstErrors)
		}
		if len(run.JobSeconds) != parallelFixturePackages || len(run.CheckSeconds) != parallelFixturePackages {
			test.Fatalf("timed %d jobs and %d checks, want %d of each", len(run.JobSeconds), len(run.CheckSeconds),
				parallelFixturePackages)
		}
		if run.WallSeconds <= 0 || run.Process.WallSeconds <= 0 {
			test.Errorf("the schedule's wall is %v and its measure's %v, want both above zero", run.WallSeconds,
				run.Process.WallSeconds)
		}
	}
}

// TestParallelChecksProveOneBuild runs the parallel fixture through the whole spike with --check-workers for its host
// alone: the variational check on the engine's schedule, its answers projected and proved green against the oracle,
// and the host's plain check on the same schedule beside it, every package of both timed.
func TestParallelChecksProveOneBuild(test *testing.T) {
	test.Parallel()
	asked, parseError := parseCommandLine([]string{
		"--root", parallelFixtureRoot,
		"--host", "darwin/arm64,cgo=0",
		"--check-workers", strconv.Itoa(parallelWorkers),
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	report, spikeError := runSpike(asked, io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if !report.green() {
		test.Errorf("the parallel fixture's proof is red:\n%s", tableText(report))
	}
	if report.Parallel == nil {
		test.Fatal("--check-workers asked for a parallel check, and the report holds none")
	}
	if report.Parallel.HostPlainWarmSeconds <= 0 {
		test.Errorf("the host plain warm-up took %vs, want some time", report.Parallel.HostPlainWarmSeconds)
	}
	if report.Parallel.VariationalWarmSeconds <= 0 || len(report.Parallel.VariationalWarmed.JobSeconds) !=
		parallelFixturePackages {
		test.Errorf("the warmed variational schedule: warm-up %vs, %d jobs, want some time and %d",
			report.Parallel.VariationalWarmSeconds, len(report.Parallel.VariationalWarmed.JobSeconds),
			parallelFixturePackages)
	}
	// Merged whole before it, every package not ours leaves the warmed schedule nothing to read or merge.
	if report.Parallel.VariationalWarmedDecodeSeconds != 0 || report.Parallel.VariationalWarmedMergeSeconds != 0 {
		test.Errorf("the warmed schedule decoded %vs and merged %vs, want nothing",
			report.Parallel.VariationalWarmedDecodeSeconds, report.Parallel.VariationalWarmedMergeSeconds)
	}
	if report.Parallel.Workers != parallelWorkers {
		test.Errorf("%d workers reported, want %d", report.Parallel.Workers, parallelWorkers)
	}
	for _, row := range []struct {
		name string
		run  parallelRun
	}{{"variational", report.Parallel.Variational}, {"host plain", report.Parallel.HostPlain}} {
		if len(row.run.JobSeconds) != parallelFixturePackages || row.run.WallSeconds <= 0 {
			test.Errorf("%s: %d jobs timed over %vs, want %d over some time", row.name, len(row.run.JobSeconds),
				row.run.WallSeconds, parallelFixturePackages)
		}
	}
	if len(report.Proof) != 1 || report.Proof[0].Compared == 0 {
		test.Errorf("the proof compared nothing: %+v", report.Proof)
	}
	requireEveryMapGreen(test, report, mapExpectations{instances: true})
	// The parallel schedule measures CPU across itself, never by phase, so no row may read as no CPU spent.
	table := tableText(report)
	if !report.Timing.VariationalOnSchedule || strings.Contains(table, " 0.00x") || !strings.Contains(table, "n/a") {
		test.Errorf("the table prints the parallel variational rows' unmeasured cpu as a number:\n%s", table)
	}
}

// TestParallelChecksProveEveryBuild runs the parallel fixture through the whole spike with --check-workers for darwin,
// linux and windows: the variational check of every package on the engine's schedule, many at once through one merged
// importer, platform's and status's answers forking by build and crossing into uses, each build's projection compared
// with go/packages' plain answers. Under `-race` a check reading what another writes fails the test.
func TestParallelChecksProveEveryBuild(test *testing.T) {
	test.Parallel()
	asked, parseError := parseCommandLine([]string{
		"--root", parallelFixtureRoot,
		"--host", "darwin/arm64,cgo=0",
		"--build", "linux/arm64",
		"--build", "windows/arm64",
		"--check-workers", strconv.Itoa(parallelWorkers),
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	report, spikeError := runSpike(asked, io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if !report.green() {
		test.Errorf("the parallel fixture's proof is red:\n%s", tableText(report))
	}
	if len(report.Proof) != 3 {
		test.Fatalf("%d builds proved, want 3", len(report.Proof))
	}
	for _, proof := range report.Proof {
		if proof.Compared == 0 || proof.Matched != proof.Compared {
			test.Errorf("%s: %d of %d matched", proof.Target, proof.Matched, proof.Compared)
		}
	}
	if report.Totals.ForkedExpressions == 0 {
		test.Error("the fixture's files differ by build, yet no expression has more than one answer")
	}
	requireEveryMapGreen(test, report, mapExpectations{overlaid: true, instances: true})
	forkedIn := map[string]bool{}
	for _, our := range report.Ours {
		forkedIn[our.Path] = our.ForkedExpressions != 0
	}
	for _, path := range []string{"example.com/parallel/platform", "example.com/parallel/uses"} {
		if !forkedIn[path] {
			test.Errorf("%s has no expression with more than one answer, though its builds read other types", path)
		}
	}
	if report.Parallel == nil {
		test.Fatal("--check-workers asked for a parallel check, and the report holds none")
	}
	if report.Parallel.HostPlainWarmSeconds <= 0 {
		test.Errorf("the host plain warm-up took %vs, want some time", report.Parallel.HostPlainWarmSeconds)
	}
	if report.Parallel.VariationalWarmSeconds <= 0 || len(report.Parallel.VariationalWarmed.JobSeconds) !=
		parallelFixturePackages {
		test.Errorf("the warmed variational schedule: warm-up %vs, %d jobs, want some time and %d",
			report.Parallel.VariationalWarmSeconds, len(report.Parallel.VariationalWarmed.JobSeconds),
			parallelFixturePackages)
	}
	// Merged whole before it, every package not ours leaves the warmed schedule nothing to read or merge.
	if report.Parallel.VariationalWarmedDecodeSeconds != 0 || report.Parallel.VariationalWarmedMergeSeconds != 0 {
		test.Errorf("the warmed schedule decoded %vs and merged %vs, want nothing",
			report.Parallel.VariationalWarmedDecodeSeconds, report.Parallel.VariationalWarmedMergeSeconds)
	}
	for _, row := range []struct {
		name string
		run  parallelRun
	}{{"variational", report.Parallel.Variational}, {"host plain", report.Parallel.HostPlain}} {
		if len(row.run.JobSeconds) != parallelFixturePackages || row.run.WallSeconds <= 0 {
			test.Errorf("%s: %d jobs timed over %vs, want %d over some time", row.name, len(row.run.JobSeconds),
				row.run.WallSeconds, parallelFixturePackages)
		}
	}
}

// cgoFixtureRoot is native, which adds through C where its build runs cgo and in Go where it doesn't, and use, which
// reads native's declarations, C's types among them, from another package of ours.
var cgoFixtureRoot = filepath.Join("testdata", "cgo")

// TestCgoPackagesProveEveryBuild runs the cgo fixture through the whole spike, in parallel, for the host with cgo on
// and linux with it off: the host's native is cgo's generated files, as go list -compiled and go/packages both give
// them, linux's its pure Go file, and each build's projection must match go/packages' plain answers exactly.
func TestCgoPackagesProveEveryBuild(test *testing.T) {
	test.Parallel()
	asked, parseError := parseCommandLine([]string{
		"--root", cgoFixtureRoot,
		"--host", "darwin/arm64,cgo=1",
		"--build", "linux/arm64",
		"--check-workers", "2",
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	report, spikeError := runSpike(asked, io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if !report.green() {
		test.Errorf("the cgo fixture's proof is red:\n%s", tableText(report))
	}
	if len(report.Proof) != 2 {
		test.Fatalf("%d builds proved, want 2", len(report.Proof))
	}
	for _, proof := range report.Proof {
		if proof.Compared == 0 || proof.Matched != proof.Compared {
			test.Errorf("%s: %d of %d matched", proof.Target, proof.Matched, proof.Compared)
		}
	}
	requireEveryMapGreen(test, report, mapExpectations{overlaid: true})
	for _, our := range report.Ours {
		// cgo's files for the host, at least its types and the file it rewrote, and the pure file for linux.
		if our.Path == "example.com/cgo/native" && (our.Files < 3 || our.FilesInEveryBuild != 0) {
			test.Errorf("native has %d files, %d in every build, want cgo's generated files for the host and the "+
				"pure file for linux, none shared", our.Files, our.FilesInEveryBuild)
		}
	}
}

// mapExpectations are what a fixture's Info map proof must hold beyond every map matching: overlays for every build
// but the host, when the fixture's builds differ, and instances compared, when the fixture instantiates generics.
type mapExpectations struct {
	overlaid  bool
	instances bool
}

// requireEveryMapGreen requires every build's every Info map to match the oracle's, Defs and Uses to have compared
// something, and what expected says, logging each map's counts.
func requireEveryMapGreen(test *testing.T, report *spikeReport, expected mapExpectations) {
	test.Helper()
	instances := 0
	for index, proof := range report.Proof {
		if len(proof.Maps) != len(infoMapNames) {
			test.Fatalf("%s: %d maps proved, want %d", proof.Target, len(proof.Maps), len(infoMapNames))
		}
		overlay := 0
		for _, mapped := range proof.Maps {
			test.Logf("%s %s: %d compared, %d matched, %d mismatched, %d only projected, %d only plain, overlay %d",
				proof.Target, mapped.Map, mapped.Compared, mapped.Matched, mapped.Mismatched, mapped.OnlyProjected,
				mapped.OnlyPlain, mapped.Overlay)
			if !mapped.Green {
				test.Errorf("%s %s is red: %+v", proof.Target, mapped.Map, mapped.FirstDifferences)
			}
			if (mapped.Map == infoMapDefs || mapped.Map == infoMapUses) && mapped.Compared == 0 {
				test.Errorf("%s %s compared nothing", proof.Target, mapped.Map)
			}
			if mapped.Map == infoMapInstances {
				instances += mapped.Compared
			}
			overlay += mapped.Overlay
		}
		if expected.overlaid && index > 0 && overlay == 0 {
			test.Errorf("%s differs from the host, yet its overlay holds nothing", proof.Target)
		}
	}
	if expected.instances && instances == 0 {
		test.Error("the fixture instantiates generics, yet no build compared an instance")
	}
}

// TestWrongOverlayShowsRed runs the parallel fixture for three builds with each build's Info maps projected through
// the next build's overlay, the host's through linux's and windows' through none: every build's maps must then show
// red while Types, which no overlay touches, stays green. TestParallelChecksProveEveryBuild is the same run with each
// build's own overlay, green.
func TestWrongOverlayShowsRed(test *testing.T) {
	test.Parallel()
	asked, parseError := parseCommandLine([]string{
		"--root", parallelFixtureRoot,
		"--host", "darwin/arm64,cgo=0",
		"--build", "linux/arm64",
		"--build", "windows/arm64",
		"--check-workers", strconv.Itoa(parallelWorkers),
		"--plant-wrong-overlay",
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	report, spikeError := runSpike(asked, io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if report.green() {
		test.Fatal("a wrong build's overlay left the run green")
	}
	for _, proof := range report.Proof {
		if proof.Mismatched != 0 || proof.OnlyProjected != 0 || proof.OnlyPlain != 0 {
			test.Errorf("%s: Types turned red, though no overlay reaches it", proof.Target)
		}
		if mapsGreen(proof.Maps) {
			test.Errorf("%s: every map matched through another build's overlay", proof.Target)
		}
	}
}
