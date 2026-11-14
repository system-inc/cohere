package go_engine

import (
	"os"
	"path/filepath"
	"strconv"
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
