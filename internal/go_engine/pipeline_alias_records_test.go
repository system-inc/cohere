package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aliasRecordsFixture is platform, whose Settings holds a part of an unnamed struct type with a count no importer can
// name, and whose Holder holds a Lock under a name every build declares alike, and reader, which compares the part, so
// its record holds what comparing the part reaches through the part's type, platform's count's type, in full
// (#6a3kjvz piece 4). A record names another package's objects, so their own units judge them in a later run, but the
// bits a read reaches through an unnamed type of another package are walked when the record is written: that is
// what a record keeps of an import's types that can go stale.
var aliasRecordsFixture = filepath.Join("testdata", "alias-records")

// aliasRecordsReader is the fixture's reader.
const aliasRecordsReader = "example.com/aliasrecords/reader"

// editPlatform replaces was with now in root's platform/settings.go, failing when it does not hold was.
func editPlatform(test *testing.T, root string, was string, now string) {
	test.Helper()
	file := filepath.Join(root, "platform", "settings.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	edited := strings.Replace(string(contents), was, now, 1)
	if edited == string(contents) {
		test.Fatalf("settings.go holds no %q", was)
	}
	if writeError := os.WriteFile(file, []byte(edited), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// makeBodyOnly edits the body of platform's Own alone, which moves platform's key and so reader's, while platform's
// API and facts stay as they were.
func makeBodyOnly(test *testing.T, root string) {
	test.Helper()
	editPlatform(test, root, "return fmt.Sprint(Settings.Part.count)",
		"return fmt.Sprintf(\"%v\", Settings.Part.count)")
}

// holdPartCount changes the type of the part's count to a Holder, whose layout windows changes and whose name it does
// not: no importer can name the field, but one comparing the part reads its layout.
func holdPartCount(test *testing.T, root string) {
	test.Helper()
	editPlatform(test, root, "Part struct{ count base.Count }", "Part struct{ count Holder }")
}

// coldVerdictsOf are the verdicts of a cold run over a fresh copy of the alias-records fixture, edited by edit.
func coldVerdictsOf(test *testing.T, edit func(*testing.T, string)) map[string]bool {
	test.Helper()
	edited := copyFixture(test, aliasRecordsFixture)
	edit(test, edited)
	cold, _ := runOverRaw(test, edited, exitClean, recordsSwitches...)
	return graphVerdicts(test, cold, edited)
}

// A hit by an alias reads the record beside its target (engine's ruling 1 on #6a3kjvz, piece 4): after a body-only
// edit of platform's Own, the first warm run checks platform again, and restores reader by its second key, since
// platform's output did not move, aliasing reader's new key to its old entry; the second warm run restores reader
// through that alias and reads the record beside the old entry. Each warm run's graph is a cold run's of the edited
// tree, unit for unit and verdict for verdict, in every build.
func TestAnAliasHitReadsItsTargetsRecord(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, aliasRecordsFixture)
	runOverRaw(test, root, exitClean, recordsSwitches...)
	makeBodyOnly(test, root)
	first, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
	second, secondStream := runOverRaw(test, root, exitClean, recordsSwitches...)
	coldVerdicts := coldVerdictsOf(test, makeBodyOnly)
	expectSameVerdicts(test, "the first warm run against cold", coldVerdicts, graphVerdicts(test, first, root))
	expectSameVerdicts(test, "the second warm run against cold", coldVerdicts, graphVerdicts(test, second, root))
	if counted := recordCounts(test, secondStream); counted.AliasRead != 1 || counted.AliasSkipped != 0 ||
		counted.Written != 0 || counted.Backfilled != 0 {
		test.Errorf("the second warm run counts %+v, want reader's record read through its alias and nothing "+
			"skipped, written or backfilled", counted)
	}
}

// A change to the type of a field no importer can name is never read stale through an alias (engine's ruling 1 on
// #6a3kjvz, piece 4): the part's count made a Holder moves platform's export rendering, which writes every struct
// field, so reader's second key misses, the first warm run checks reader from source and writes its record, and the
// second warm run's graph is a cold run's of the edited tree, where reader, comparing a part that holds a Lock, is
// dependent on windows. A rendering that left unexported fields out would alias reader to its old entry, and the
// second warm run would read the record whose part held a count, which windows lays out as every build does.
func TestAnUnexportedFieldsTypeIsNeverReadStaleThroughAnAlias(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, aliasRecordsFixture)
	runOverRaw(test, root, exitClean, recordsSwitches...)
	holdPartCount(test, root)
	runOverRaw(test, root, exitClean, recordsSwitches...)
	second, _ := runOverRaw(test, root, exitClean, recordsSwitches...)
	coldVerdicts := coldVerdictsOf(test, holdPartCount)
	dependent := false
	for unit, isDependent := range coldVerdicts {
		dependent = dependent || isDependent && strings.Contains(unit, " "+aliasRecordsReader+" ")
	}
	if !dependent {
		test.Fatal("no build calls reader dependent in the edited tree, so the case proves nothing")
	}
	expectSameVerdicts(test, "the second warm run against cold", coldVerdicts, graphVerdicts(test, second, root))
}
