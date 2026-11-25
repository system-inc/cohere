package go_language

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rules"
)

// bodyReachLackedFixture holds lacked, whose Scaled reads a constant of linked, a package the host records no uses of,
// and tool, a command with an in-package test that nothing imports, which every extra build's load leaves out
// (load.Caching's AnalyzedRootsOnly), its test variant standing in for it. A file only windows builds gives the run a
// windows build to judge.
var bodyReachLackedFixture = filepath.Join("testdata", "body-reach-lacked")

// lackedCarryLine is the census's line of the load mode carry's check (bodyReacher.startsCarry) in a run that found
// nothing: no package the build lacks whole is imported by a file it compiles.
const lackedCarryLine = "  lacked by the build and imported by a file it compiles, which starts the carry: 0 packages"

// lackedCensus is the pipeline of a run over a copy of the lacked fixture under configure, when it is not nil, with
// what windows's census gives lacked's own bodies, the reason of each by name and what reached each first, the copy's
// root taken out, and the census file's lines. The fixture's premise is checked first: the host holds tool, and
// windows's load checks tool's test variant from source and holds no tool.
func lackedCensus(
	test *testing.T,
	configure func(*pipeline),
) (on *pipeline, reasons map[string]string, drivers map[string]string, written []string) {
	test.Helper()
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	record := test.TempDir()
	root := copyFixture(test, bodyReachLackedFixture)
	on, _ = runOverConfigured(test, root, exitFindings, func(pipeline *pipeline) {
		pipeline.bodyReachCensus = censusFile
		pipeline.bodyReachFromSourceRecord = record
		if configure != nil {
			configure(pipeline)
		}
	}, "--no-cache", "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" ||
		on.buildRuns[1].bodyReach == nil {
		test.Fatalf("want the host's build and windows's census, got %d runs", len(on.buildRuns))
	}
	if _, isHosted := on.dependence.paths["example.com/lacked/tool"]; !isHosted {
		test.Fatal("the host holds no tool, so nothing is lacked")
	}
	checked := on.buildRuns[1].fromSourceIdentifiers
	if slices.Contains(checked, "example.com/lacked/tool") ||
		!slices.Contains(checked, "example.com/lacked/tool [example.com/lacked/tool.test]") {
		test.Fatalf("want windows to check tool's test variant and no tool, got %v", checked)
	}
	reasons, drivers = map[string]string{}, map[string]string{}
	for _, body := range on.buildRuns[1].bodyReach.bodies {
		if body.packageIdentifier == "example.com/lacked" {
			reasons[body.name] = cmp.Or(string(body.reason), "unreached")
			drivers[body.name] = strings.ReplaceAll(body.driver, root+string(filepath.Separator), "")
		}
	}
	contents, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	return on, reasons, drivers, strings.Split(string(contents), "\n")
}

// A package the host holds and the build does not starts no carry (#wzre5eg). The host holds tool, a command whose
// test variant stands in for it in windows's load, and the packages of the standard library that only the host's
// platform compiles, which windows holds nothing of. Both builds check every package they share from source, so no
// value holds another load's: Scaled, which reads linked's Scale, a constant of a package the host recorded no uses of,
// stays unreached, and the load mode reaches no body of the build. Read by its ID alone, each package the build lacks
// was loaded in another mode, and carried every such constant in every run. The check's line reads 0: no file windows
// compiles imports a package it holds nothing of.
func TestAPackageTheBuildLacksStartsNoCarry(test *testing.T) {
	test.Parallel()

	on, reasons, _, written := lackedCensus(test, nil)
	if want := map[string]string{"Scaled": "unreached", "Plain": "unreached", "Mode": "Constant"}; !maps.Equal(reasons,
		want) {
		test.Errorf("windows's census judges lacked's bodies\n%v\nwant\n%v", reasons, want)
	}
	if byLoadMode := on.buildRuns[1].bodyReach.byReason[reachedLoadMode]; byLoadMode != 0 {
		test.Errorf("the load mode reaches %d bodies of a build that loads every package as the host does, want none",
			byLoadMode)
	}
	// Nor through a signature, which the census writes as Signature.
	for _, body := range on.buildRuns[1].bodyReach.bodies {
		if body.partReason == reachedLoadMode {
			test.Errorf("%s is reached by the load mode, %s", body, body.driver)
		}
	}
	if !slices.Contains(written, lackedCarryLine) {
		test.Errorf("the census file has no line %q:\n%s", lackedCarryLine, strings.Join(written, "\n"))
	}
}

// A package the two sides load in different modes still starts the carry, beside the packages the build lacks
// (#wzre5eg): over the same fixture the host restores the standard library cohere-go tests' table holds from the
// system cache, export data, and windows, judging dependence, checks it from source. Scale may hold another load's
// value for all the reach can tell of linked, so Scaled is reached by the load mode, as before. The check's line still
// reads 0.
func TestAPackageLoadedOtherwiseStillStartsTheCarryBesideALackedOne(test *testing.T) {
	test.Parallel()

	filled, _ := filledSystemCache(test, rules.All(), testsStamp, nil)
	on, reasons, drivers, written := lackedCensus(test, readingSystemCache(filled, ""))
	if !on.dependence.exportData["fmt"] || !slices.Contains(on.buildRuns[1].fromSourceIdentifiers, "fmt") {
		test.Fatalf("want fmt read from export data on the host and checked from source on windows, got %t and %v",
			on.dependence.exportData["fmt"], on.buildRuns[1].fromSourceIdentifiers)
	}
	if want := map[string]string{"Scaled": "LoadMode", "Plain": "unreached", "Mode": "Constant"}; !maps.Equal(reasons,
		want) {
		test.Errorf("windows's census judges lacked's bodies\n%v\nwant\n%v", reasons, want)
	}
	if want := "example.com/lacked/linked.Scale (linked/linked.go) LoadMode load mode in the body"; drivers["Scaled"] !=
		want {
		test.Errorf("Scaled is reached first by %q, want %q", drivers["Scaled"], want)
	}
	if !slices.Contains(written, lackedCarryLine) {
		test.Errorf("the census file has no line %q:\n%s", lackedCarryLine, strings.Join(written, "\n"))
	}
}

// The census says which rule reached each body it writes as Signature (#wzre5eg), a label that hid that every one of
// 19,318 at cohere's root was the load mode's. Over the body-reach fixture, Accepted and Shifted take a Number, whose
// name bit is set, and Bounded, added here, an array sized by platformLimit, a constant each build's own file declares:
// three bodies reached by their signatures, two by a name and one by a constant's value.
func TestTheCensusCountsASignaturesReachByItsOwnReason(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	if writeError := os.WriteFile(filepath.Join(root, "bounded.go"), []byte(`package reach

// Bounded takes an array sized by a constant each build's own file declares.
func Bounded(values [platformLimit]int) int {
	return values[0]
}
`), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, root, exitFindings, func(pipeline *pipeline) {
		pipeline.bodyReachCensus = censusFile
	}, "--no-cache", "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyReach == nil {
		test.Fatalf("want windows's census, got %d runs", len(on.buildRuns))
	}
	if signatures := on.buildRuns[1].bodyReach.byReason[reachedSignature]; signatures != 3 {
		test.Fatalf("want Accepted, Shifted and Bounded reached by their signatures, got %d", signatures)
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	want := "  reached by Signature, by the reason its signature gave: Sizes 0, Unrecorded 0, Miscounted 0, Unknown " +
		"0, Signature 0, Named 2, Constant 1, LoadMode 0, Selected 0, ValueType 0, TypeArgument 0, Shadowed 0, " +
		"LineClaim 0, Imports 0, ScopeName 0, Initializers 0"
	if !slices.Contains(strings.Split(string(written), "\n"), want) {
		test.Errorf("the census file has no line %q:\n%s", want, written)
	}
}
