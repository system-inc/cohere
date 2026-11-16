package go_engine

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// standardLibraryRecordsFixture reaches errors, fmt, strings and unicode/utf8, which the engine tests' table holds, and
// has a file only windows builds, so a run under `--other-builds` judges a windows build.
var standardLibraryRecordsFixture = filepath.Join("testdata", "std-records")

// readingTableAs is the binary's entries as a run reads table under stamp, its standard library's dependence records
// keyed by toolchain in place of go env's when it is not empty.
func readingTableAs(table []byte, stamp string, toolchain string) func(*pipeline) {
	return func(engine *pipeline) {
		engine.request.standardLibrary = builtInStandardLibrary{table: cache.NewStandardLibraryTable(table),
			stamp: stamp, toolchain: toolchain}
	}
}

// everyGraphVerdict are every unit of every build's judgment, of the host's graph and the standard library's, by
// build, graph, package, file and start, and whether the build makes it dependent.
func everyGraphVerdict(test *testing.T, engine *pipeline) map[string]bool {
	test.Helper()
	verdicts := map[string]bool{}
	for _, run := range engine.buildRuns {
		if run.judged == nil || run.judged.dependence == nil {
			continue
		}
		for _, unit := range run.judged.dependence.Units() {
			verdicts[run.build.String()+" host "+unit.PackageId+" "+unit.File+":"+strconv.Itoa(unit.Start)] =
				unit.Dependent
		}
		if run.judged.standardLibrary == nil {
			continue
		}
		for _, unit := range run.judged.standardLibrary.Units() {
			verdicts[run.build.String()+" std "+unit.PackageId+" "+unit.File+":"+strconv.Itoa(unit.Start)] =
				unit.Dependent
		}
	}
	if len(verdicts) == 0 {
		test.Fatal("no build was judged")
	}
	return verdicts
}

// restoredOf is the stream's first build's judgment of what the host restored, failing when there is none.
func restoredOf(test *testing.T, stream string) contract.RestoredDependence {
	test.Helper()
	for _, build := range lintRecordOf(test, stream).Builds {
		if build.Dependence != nil && build.Dependence.Restored != nil {
			return *build.Dependence.Restored
		}
	}
	test.Fatal("no build judged what the host restored")
	return contract.RestoredDependence{}
}

// The standard library's records come from the project's store once a run has side-loaded it (#6a3kjvz piece 3):
// over the engine tests' table, a cold run under `--build-dependence` loads the standard library it restored from the
// table beside the host's lint and writes each package's record into the store; the warm run reads every one back,
// loads nothing, and judges every unit of both graphs, in every build, as the cold run did.
func TestTheStandardLibrarysRecordsComeFromTheStore(test *testing.T) {
	test.Parallel()

	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryRecordsFixture)
	reading := readingTableAs(table, engineTestsStamp, "")
	cold, coldStream, _ := runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	warm, warmStream, _ := runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	coldCounts, warmCounts := recordCounts(test, coldStream), recordCounts(test, warmStream)
	coldRestored, warmRestored := restoredOf(test, coldStream), restoredOf(test, warmStream)
	if coldCounts.StandardLibraryWritten == 0 || coldCounts.StandardLibraryRead != 0 ||
		coldRestored.StandardLibrarySideLoaded == 0 || coldRestored.StandardLibraryFromRecords != 0 {
		test.Errorf("the cold run counts %+v and %+v, want the standard library side-loaded and its records written",
			coldCounts, coldRestored)
	}
	if warmCounts.StandardLibraryRead != coldCounts.StandardLibraryWritten || warmCounts.StandardLibraryWritten != 0 ||
		warmRestored.StandardLibrarySideLoaded != 0 ||
		warmRestored.StandardLibraryFromRecords != coldCounts.StandardLibraryWritten {
		test.Errorf("the warm run counts %+v and %+v, want the %d records read, the side load skipped",
			warmCounts, warmRestored, coldCounts.StandardLibraryWritten)
	}
	expectSameVerdicts(test, "warm against cold", everyGraphVerdict(test, cold), everyGraphVerdict(test, warm))
}

// A standard-library record another toolchain wrote is never read (@system_cohere's condition on #6a3kjvz piece 3):
// a run whose records are keyed under one toolchain leaves a run under another reading none, side-loading the
// standard library again and writing its records under its own.
func TestAStandardLibraryRecordOfAnotherToolchainIsNeverRead(test *testing.T) {
	test.Parallel()

	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryRecordsFixture)
	_, firstStream, _ := runOverNoted(test, root, exitClean, readingTableAs(table, engineTestsStamp, "go1.0 /one"),
		recordsSwitches...)
	_, otherStream, _ := runOverNoted(test, root, exitClean, readingTableAs(table, engineTestsStamp, "go1.0 /two"),
		recordsSwitches...)
	written := recordCounts(test, firstStream).StandardLibraryWritten
	other, otherRestored := recordCounts(test, otherStream), restoredOf(test, otherStream)
	if written == 0 || other.StandardLibraryRead != 0 || other.StandardLibraryWritten != written ||
		otherRestored.StandardLibrarySideLoaded == 0 {
		test.Errorf("under another toolchain the run counts %+v and %+v, want none of the first one's %d records "+
			"read, the standard library side-loaded and its records written again", other, otherRestored, written)
	}
}

// A run under `--no-cache` has no store to read records from, and side-loads the standard library as it always did.
func TestANoCacheRunSideLoadsTheStandardLibrary(test *testing.T) {
	test.Parallel()

	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryRecordsFixture)
	reading := readingTableAs(table, engineTestsStamp, "")
	runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	_, stream, _ := runOverNoted(test, root, exitClean, reading, append(recordsSwitches, "--no-cache")...)
	if restored := restoredOf(test, stream); restored.StandardLibrarySideLoaded == 0 ||
		restored.StandardLibraryFromRecords != 0 {
		test.Errorf("the run under --no-cache counts %+v, want the standard library side-loaded", restored)
	}
	if lintRecordOf(test, stream).Cache.DependenceRecords != nil {
		test.Error("a run under --no-cache counts dependence records")
	}
}

// A standard-library record of a file whose bytes have changed since it was written is never read (engine's ruling on
// #6a3kjvz piece 3): in a copy of the local Go installation, a run under `--build-dependence` writes the standard
// library's records, and once unicode/utf8's utf8.go gains a declaration, a run reading the store judges every unit of
// both graphs, in every build, as a run under `--no-cache` does, which side-loads every package it restored. utf8 and
// its importers miss the binary's table, whose keys hold each file's bytes, and so does each record keyed from one.
//
// Not parallel: it sets the process's own `GOROOT`, which every go command the engine runs reads.
func TestAStandardLibraryRecordOfAnEditedFileIsNeverRead(test *testing.T) {
	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryRecordsFixture)
	mirror := mirroredGoRoot(test, goEnvironmentIn(test, root, nil)["GOROOT"], "src/unicode/utf8/utf8.go", "")
	test.Setenv("GOROOT", mirror)
	reading := readingTableAs(table, engineTestsStamp, "")
	_, coldStream, _ := runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	if written := recordCounts(test, coldStream).StandardLibraryWritten; written == 0 {
		test.Fatal("the cold run wrote no standard-library record")
	}
	utf8File := filepath.Join(mirror, "src", "unicode", "utf8", "utf8.go")
	edited, openError := os.OpenFile(utf8File, os.O_APPEND|os.O_WRONLY, 0)
	if openError != nil {
		test.Fatal(openError)
	}
	_, writeError := edited.WriteString(
		"\n// changedByTheTest is a declaration no record was written with.\nfunc changedByTheTest() {}\n")
	if closeError := edited.Close(); writeError != nil || closeError != nil {
		test.Fatalf("editing utf8.go: %v, %v", writeError, closeError)
	}
	warm, warmStream, _ := runOverNoted(test, root, exitClean, reading, recordsSwitches...)
	sideLoaded, _, _ := runOverNoted(test, root, exitClean, reading, append(recordsSwitches, "--no-cache")...)
	if read := recordCounts(test, warmStream).StandardLibraryRead; read == 0 {
		test.Fatal("the warm run read no standard-library record, so its verdicts prove nothing")
	}
	expectSameVerdicts(test, "read from the store against side-loaded", everyGraphVerdict(test, sideLoaded),
		everyGraphVerdict(test, warm))
}

// expectSameCensus fails for every build whose facts census in got, whose standard-library graph is records, does not
// count what want's, side-loaded, counts as differing: kind for kind the facts that differ and the answers asked in one
// build alone, and every unit those reach, firstOrderUnits among them. got may compare fewer facts than want, since it
// leaves out what no name pairs exactly (pairByUnit), but never more, and never more fewer than its Unkeyed
// counts, its parts summing to it; it logs how many it left out, by why, and returns those by build (#h660y7d).
func expectSameCensus(test *testing.T, want string, got string) map[string]contract.UnkeyedFacts {
	test.Helper()
	parts := map[string]contract.UnkeyedFacts{}
	wantBuilds, gotBuilds := lintRecordOf(test, want).Builds, lintRecordOf(test, got).Builds
	compared := 0
	for index := range min(len(wantBuilds), len(gotBuilds)) {
		wantDependence, gotDependence := wantBuilds[index].Dependence, gotBuilds[index].Dependence
		if wantDependence == nil || gotDependence == nil || wantDependence.Facts == nil || gotDependence.Facts == nil {
			continue
		}
		wantFacts, gotFacts := *wantDependence.Facts, *gotDependence.Facts
		target := gotBuilds[index].Target
		if wantFacts.Workspace != gotFacts.Workspace || wantFacts.Dependencies != gotFacts.Dependencies {
			test.Errorf("build %s: the facts census reaches\n%+v and %+v\nwant\n%+v and %+v", target,
				gotFacts.Workspace, gotFacts.Dependencies, wantFacts.Workspace, wantFacts.Dependencies)
		}
		gotKinds := map[[2]string]contract.FactKindCounts{}
		for _, kind := range gotFacts.Kinds {
			gotKinds[[2]string{kind.Analyzer, kind.FactType}] = kind
		}
		fewer := 0
		for _, wantKind := range wantFacts.Kinds {
			gotKind := gotKinds[[2]string{wantKind.Analyzer, wantKind.FactType}]
			if gotKind.Different != wantKind.Different || gotKind.WorkspaceDifferent != wantKind.WorkspaceDifferent ||
				gotKind.AskedInOne != wantKind.AskedInOne || gotKind.Compared > wantKind.Compared {
				test.Errorf("build %s: %s %s counts %+v, want %+v", target, wantKind.Analyzer, wantKind.FactType,
					gotKind, wantKind)
			}
			fewer += wantKind.Compared - gotKind.Compared
		}
		if fewer > gotFacts.Unkeyed {
			test.Errorf("build %s: the census compares %d facts fewer than the side load and counts %d unkeyed",
				target, fewer, gotFacts.Unkeyed)
		}
		unkeyed := gotFacts.UnkeyedParts
		if total := unkeyed.Unnamed + unkeyed.Closures + unkeyed.Unplaced + unkeyed.PathOnly + unkeyed.Collisions +
			unkeyed.Answers; total != gotFacts.Unkeyed {
			test.Errorf("build %s: the census counts %d unkeyed, and its parts %+v sum to %d", target,
				gotFacts.Unkeyed, unkeyed, total)
		}
		parts[target] = unkeyed
		test.Logf("build %s: %d facts compared fewer, %d unkeyed, %+v", target, fewer, gotFacts.Unkeyed,
			gotFacts.UnkeyedParts)
		compared++
	}
	if compared == 0 {
		test.Error("no build's facts census was compared")
	}
	return parts
}

// The facts census counts the same differing facts, and the same units they reach, whether the standard library's
// graph comes from the records the findings store keeps (#6a3kjvz piece 3) or from a side load (#01chv64): a cold run
// over a table with no records side-loads the standard library and writes its records; a fresh copy of the fixture
// whose store holds those records alone reads them, with its host checked from source as the first run's was, and
// both runs census every build alike (expectSameCensus), judge every unit alike and report the same findings.
func TestTheStandardLibrarysStoredRecordsKeepTheFactsCensus(test *testing.T) {
	test.Parallel()

	table, _ := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	reading := readingTableAs(table, engineTestsStamp, "")
	sideRoot := copyFixture(test, standardLibraryRecordsFixture)
	sideLoaded, sideStream, _ := runOverNoted(test, sideRoot, exitClean, reading, recordsSwitches...)
	if restored := restoredOf(test, sideStream); restored.StandardLibrarySideLoaded == 0 {
		test.Fatalf("the first run counts %+v, want the standard library side-loaded", restored)
	}
	storedRoot := copyFixture(test, standardLibraryRecordsFixture)
	_, standardLibraryKeys := storedRecordKeys(test, sideRoot)
	for key := range standardLibraryKeys {
		name := hex.EncodeToString(key[:])
		contents, readError := os.ReadFile(filepath.Join(storeDirectory(sideRoot), name[:2], name))
		if readError != nil {
			test.Fatal(readError)
		}
		copied := filepath.Join(storeDirectory(storedRoot), name[:2], name)
		if makeError := os.MkdirAll(filepath.Dir(copied), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(copied, contents, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	fromStore, storeStream, _ := runOverNoted(test, storedRoot, exitClean, reading, recordsSwitches...)
	if restored := restoredOf(test, storeStream); restored.StandardLibraryFromRecords == 0 ||
		restored.StandardLibrarySideLoaded != 0 {
		test.Fatalf("over the stored records the run counts %+v, want every package from them", restored)
	}
	withoutRoot := func(verdicts map[string]bool, root string) map[string]bool {
		moved := make(map[string]bool, len(verdicts))
		for unit, dependent := range verdicts {
			moved[strings.ReplaceAll(unit, root, "")] = dependent
		}
		return moved
	}
	expectSameVerdicts(test, "from the store against side-loaded",
		withoutRoot(everyGraphVerdict(test, sideLoaded), sideRoot), withoutRoot(everyGraphVerdict(test, fromStore),
			storedRoot))
	// The fixture's windows build meets facts the host holds by a member's path alone, and places every answer, its
	// parameters' among them, through their functions' units.
	if parts := expectSameCensus(test, sideStream, storeStream)["windows/arm64"]; parts.PathOnly == 0 ||
		parts.Answers != 0 {
		test.Errorf("the windows build leaves out %+v, want facts held by a path alone and every answer placed", parts)
	}
	withoutRootLines := func(lines []string, root string) []string {
		moved := make([]string, 0, len(lines))
		for _, line := range lines {
			moved = append(moved, strings.ReplaceAll(line, root, ""))
		}
		return moved
	}
	if !slices.Equal(withoutRootLines(findingLines(sideStream), sideRoot),
		withoutRootLines(findingLines(storeStream), storedRoot)) {
		test.Error("the findings over the stored records are not the side-loaded run's")
	}
}
