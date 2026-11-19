package build_reads

import (
	"go/types"
	"slices"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/tools/go/types/objectpath"
)

// recordedUses is checked's record of what its declarations use, failing test when it is unrecorded.
func recordedUses(test *testing.T, checked checkedFixturePackage) *FunctionUses {
	test.Helper()

	reads := &Reads{}
	reads.RecordTypes(checked.own, checked.source.info, checked.source.files)
	uses, recorded := reads.FunctionUses()
	if !recorded {
		test.Fatalf("want %s's function uses recorded", checked.own.Path())
	}
	return uses
}

// partsUnknown are, by declaration in the order the record yields them, whether its signature and its body are
// unknown, written `signature body`, each `true` or `false`.
func partsUnknown(record *FunctionUses) []string {
	var written []string
	for declared := range record.Declarations() {
		written = append(written, declared.Declaration.Name+" "+strconv.FormatBool(declared.SignatureUnknown)+" "+
			strconv.FormatBool(declared.BodyUnknown))
	}
	return written
}

// encodedUnknown is whether object's identity is unknown by its objectpath alone, the control the record's judgment
// is compared to: what encoder fails to place is unknown, but for the kinds no objectpath names and every build names
// alike by what they hold, an import's name, a universe object, error's Error among them, and a package-level var,
// const or func of the package's own, whose path objectpath refuses only because no importer can reach it, the name
// alone naming it in every build.
func encodedUnknown(encoder *objectpath.Encoder, object types.Object) bool {
	if _, isPackageName := object.(*types.PkgName); isPackageName {
		return false
	}
	if object.Pkg() == nil {
		function, isFunction := object.(*types.Func)
		return types.Universe.Lookup(object.Name()) != object && (!isFunction || function.Signature().Recv() == nil)
	}
	if _, isTypeName := object.(*types.TypeName); !isTypeName && !object.Exported() &&
		object.Pkg().Scope().Lookup(object.Name()) == object {
		return false
	}
	_, pathError := encoder.For(object)
	return pathError != nil
}

// Recording a package judges nothing: no entry's identity, so no objectpath, is asked until a reader first reads the
// record, when the whole package is judged at once (#kr1m304). A run records every package it checks and reads the
// records of only those a build checks from source, so a record no build reads costs the encoder nothing.
func TestARecordIsJudgedOnlyWhenFirstRead(test *testing.T) {
	test.Parallel()

	_, packagesChecked := checkedFixture(test, unknownSources)
	record := recordedUses(test, packagesChecked[0])
	if record.objectsUnknown != nil || record.selectionsUnknown != nil || record.signaturesUnknown != nil ||
		record.bodiesUnknown != nil {
		test.Fatal("want the record unjudged until it is read, got it judged as it was recorded")
	}
	if len(record.objects) == 0 || len(record.selections) == 0 {
		test.Fatal("want the fixture's record to hold objects and selections")
	}
	_ = partsUnknown(record)
	if len(record.objectsUnknown) != len(record.objects) || len(record.selectionsUnknown) != len(record.selections) ||
		len(record.signaturesUnknown) != len(record.declarations) ||
		len(record.bodiesUnknown) != len(record.declarations) {
		test.Fatal("want the record judged whole once it is read")
	}
}

// The repeated fixture's package: cfg, whose field Values no objectpath reaches, and a const spec whose value reads
// it, constant as an array's length is, and one that repeats that value.
var repeatedSources = []fixtureFile{
	{"first", "first.go", `package first

var cfg struct{ Values [3]int }

const (
	Counted = len(cfg.Values)
	Again
)
`},
}

// A const spec that repeats another holds that one's parts, so its body is judged unknown as that one's is, though
// the walk recorded nothing of its own.
func TestARepeatingConstSpecIsJudgedAsTheSpecItRepeats(test *testing.T) {
	test.Parallel()

	_, packagesChecked := checkedFixture(test, repeatedSources)
	got := partsUnknown(recordedUses(test, packagesChecked[0]))
	want := []string{" false false", " false true", " false true"}
	if !slices.Equal(got, want) {
		test.Errorf("want each declaration's parts unknown as\n%q\ngot\n%q", want, got)
	}
}

// Several builds read one package's record at once, and each sees the one judgment, made once: run under `-race`,
// a judgment two readers made at once, or one a reader read while another wrote it, is a race.
func TestOneRecordReadByManyAtOnceIsJudgedOnce(test *testing.T) {
	test.Parallel()

	_, packagesChecked := checkedFixture(test, unknownSources)
	record := recordedUses(test, packagesChecked[0])
	const readers = 8
	read := make([][]string, readers)
	var group sync.WaitGroup
	for reader := range readers {
		group.Go(func() { read[reader] = partsUnknown(record) })
	}
	group.Wait()
	want := []string{" false false", "Read false true", "Settled false false"}
	for reader, got := range read {
		if !slices.Equal(got, want) {
			test.Errorf("reader %d: want each declaration's parts unknown as\n%q\ngot\n%q", reader, want, got)
		}
	}
}

// Every entry's judgment, by the cheap way that names a package-level object and a method by what they hold before
// it asks an objectpath, says unknown exactly where the objectpath alone does (encodedUnknown), over every package of
// every fixture: a field of another package's type, one of a type of no name, a method of each kind, and a selection's
// receiver among them.
func TestEachEntrysJudgmentIsTheObjectpathsAlone(test *testing.T) {
	test.Parallel()

	compared, unknown := 0, 0
	for _, sources := range [][]fixtureFile{functionUsesSources, structureSources, movedSources, unknownSources,
		selectionSources, branchesSources, repeatedSources} {
		_, packagesChecked := checkedFixture(test, sources)
		for _, checked := range packagesChecked {
			record := recordedUses(test, checked)
			_ = partsUnknown(record)
			var encoder objectpath.Encoder
			for entry, object := range record.objects {
				compared++
				if record.objectsUnknown[entry] {
					unknown++
				}
				if encoded := encodedUnknown(&encoder, object); record.objectsUnknown[entry] != encoded {
					test.Errorf("%s: %v judged unknown %t, its objectpath says %t", checked.own.Path(), object,
						record.objectsUnknown[entry], encoded)
				}
			}
			for entry, selection := range record.selections {
				compared++
				if record.selectionsUnknown[entry] {
					unknown++
				}
				encoded := encodedUnknown(&encoder, selection.object) ||
					selection.receiver != nil && encodedUnknown(&encoder, selection.receiver)
				if record.selectionsUnknown[entry] != encoded {
					test.Errorf("%s: the selection of %v from %v judged unknown %t, its objectpath says %t",
						checked.own.Path(), selection.object, selection.receiver, record.selectionsUnknown[entry],
						encoded)
				}
			}
		}
	}
	test.Logf("entries compared %d, judged unknown %d", compared, unknown)
	if unknown == 0 {
		test.Error("want an entry judged unknown among the fixtures, got none")
	}
}
