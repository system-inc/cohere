package check

import (
	"bytes"
	"maps"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sharedFiles are a module whose base declares an API that reaches both kinds of type gcexportdata writes once per
// pointer, so base's export data comes out in other bytes by how its dependencies were loaded, as #z14ch7c measured
// on cohere's root: key's Key is defined as hash's U128, whose struct it shares when checked from source and not when
// read from export data, and promise's method returns two functions, each with an `interface{}` of its own from
// source and the one empty interface go/types makes on import. base's Marked holds "mark", so callsAnalyzer exports
// a fact on it, and user calls it.
var sharedFiles = map[string]string{
	"go.mod":       "module example.com/shared\n\ngo 1.24\n",
	"hash/hash.go": "package hash\n\n// U128 is the struct key's Key shares.\ntype U128 struct {\n\tHi, Lo uint64\n}\n",
	"key/key.go": "package key\n\nimport \"example.com/shared/hash\"\n\n" +
		"// Key is defined as hash's U128.\ntype Key hash.U128\n",
	"promise/promise.go": "package promise\n\n// Runtime makes promises.\ntype Runtime struct{}\n\n" +
		"// NewPromise returns two functions, each with an empty interface written apart.\n" +
		"func (Runtime) NewPromise() (resolve func(result interface{}) error, reject func(reason interface{}) error) " +
		"{\n\treturn nil, nil\n}\n",
	"base/base.go": "package base\n\nimport (\n\t\"example.com/shared/hash\"\n\t\"example.com/shared/key\"\n" +
		"\t\"example.com/shared/promise\"\n)\n\n// Pair holds both of the struct's types.\ntype Pair struct {\n" +
		"\tPlain hash.U128\n\tKeyed key.Key\n}\n\n// Promises is a promise.Runtime.\n" +
		"func Promises() promise.Runtime { return promise.Runtime{} }\n\n" +
		"// Marked is what callsAnalyzer exports a fact for.\nfunc Marked() int {\n\ttotal := len(\"mark\")\n" +
		"\treturn total\n}\n",
	"user/user.go": "package user\n\nimport \"example.com/shared/base\"\n\n" +
		"// Use calls base.\nfunc Use() int {\n\t_ = base.Promises()\n\treturn base.Marked()\n}\n",
}

// sharedPackage is the package at path among a run's load.
func sharedPackage(test *testing.T, ran sessionRun, path string) *packages.Package {
	test.Helper()
	for _, loaded := range ran.loaded.Packages {
		if loaded.PkgPath == path {
			return loaded
		}
	}
	test.Fatalf("the load holds no %s", path)
	return nil
}

// withoutPositions is a package's export data as gcexportdata writes it with no positions, what the output digest
// once hashed.
func withoutPositions(test *testing.T, written *packages.Package) []byte {
	test.Helper()
	var exportData bytes.Buffer
	if writeError := gcexportdata.Write(&exportData, nil, written.Types); writeError != nil {
		test.Fatalf("writing the export data of %s: %v", written.ID, writeError)
	}
	return exportData.Bytes()
}

// base's output digest is the same whether its dependencies were checked from source, on the cold run, or read from
// the export data the cache holds, once base misses alone, though its export data's bytes differ between the two.
func TestAnOutputDigestIsTheSameOverDependenciesFromSourceAndFromExportData(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, sharedFiles)
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	caching := testCaching(test, rules)
	cold := runWithSession(test, root, caching, rules)
	coldBase := sharedPackage(test, cold, "example.com/shared/base")
	coldOutput, saved := cold.session.outputs[coldBase]
	if !saved {
		test.Fatalf("want base saved with an output by the cold run, got %+v", cold.result.Cache)
	}
	baseKey := sessionKey(test, cold.session, "example.com/shared/base")
	if putError := caching.Store.Put(baseKey, []byte("corrupt")); putError != nil {
		test.Fatalf("corrupting base's entry: %v", putError)
	}
	again := runWithSession(test, root, caching, rules)
	if again.result.Cache.Missed != 1 || again.loaded.PackagesFromExportData != 3 {
		test.Fatalf("want base missed alone over hash, key and promise read from export data, got %+v and %d read",
			again.result.Cache, again.loaded.PackagesFromExportData)
	}
	againBase := sharedPackage(test, again, "example.com/shared/base")
	if bytes.Equal(withoutPositions(test, coldBase), withoutPositions(test, againBase)) {
		test.Fatalf("want base's export data in other bytes over its dependencies from export data, the case this " +
			"test stands for, got the same bytes")
	}
	if againOutput := again.session.outputs[againBase]; againOutput != coldOutput {
		test.Fatalf("want base's output digest %x over its dependencies from export data as from source, got %x",
			coldOutput, againOutput)
	}
}

// The same facts, encoded by two processes, one of which encoded other types with gob first, so gob numbers every
// fact type differently and writes those numbers into each fact's bytes, give every package the same output digest.
func TestFactsEncodedAfterOtherGobTypesDigestAlike(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, sharedFiles)
	plain := sessionInAnotherProcess(test, root, test.TempDir(), 0)
	preceded := sessionInAnotherProcess(test, root, test.TempDir(), 3)
	if plain.MarkEncoded == preceded.MarkEncoded {
		test.Fatalf("want markFact's bytes moved by the types encoded before it, the case this test stands for, got " +
			"the same bytes")
	}
	if len(plain.Outputs) != len(sharedFiles)-1 {
		test.Fatalf("want every package saved with an output, got %v", slices.Sorted(maps.Keys(plain.Outputs)))
	}
	if !reflect.DeepEqual(plain.Outputs, preceded.Outputs) {
		test.Fatalf("want the same output digests, got\n%v\nand, after other types,\n%v", plain.Outputs,
			preceded.Outputs)
	}
}

// The first body edit after a cold run misses only the edited package, though the run is another process, whose gob
// numbers its types otherwise, and reads the edited package's dependencies from export data: user is restored by its
// second key, and the Result is a run's without the cache over the edited tree.
func TestTheFirstEditAfterAColdRunMissesOnlyTheEditedPackage(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, sharedFiles)
	store := test.TempDir()
	cold := sessionInAnotherProcess(test, root, store, 0)
	if cold.Cache.Missed != len(sharedFiles)-1 || cold.Cache.Written != cold.Cache.Missed || !cold.Agrees {
		test.Fatalf("want every package missed and written cold, as a run without the cache, got %+v, agreeing %t",
			cold.Cache, cold.Agrees)
	}
	editFile(test, root, "base/base.go", replaced(test, "\ttotal := len(\"mark\")\n",
		"\ttotal := len(\"mark\")\n\ttotal++\n"))
	edited := sessionInAnotherProcess(test, root, store, 3)
	want := CacheCounts{On: true, Hit: 4, SecondKeyHit: 1, Missed: 1, Written: 1, Waves: 1}
	if !reflect.DeepEqual(edited.Cache, want) || !edited.Agrees {
		test.Fatalf("want base missed alone and user restored by its second key, as a run without the cache, got "+
			"%+v, agreeing %t", edited.Cache, edited.Agrees)
	}
}

// A finding names another package's file only by a word whose path is a file the load holds and not its own, and a
// word whose base name no file has is passed over unjoined, except one ending in `..` or `.`, which cleaning folds
// into the elements before it and which can still name a file. A word's one trailing period is a sentence's, so
// `...` reads as `..`.
func TestAFindingNamesAForeignFileOnlyByAFileTheLoadHolds(test *testing.T) {
	test.Parallel()

	own := &packages.Package{GoFiles: []string{"/w/p/x.go"}}
	other := &packages.Package{GoFiles: []string{"/w/q/y.go"}, OtherFiles: []string{"/w/q/VERSION"}}
	known := newKnownFiles()
	known.addPackage(own)
	known.addPackage(other)
	for message, want := range map[string]bool{
		"struct field tag conflicts with a field in ../q/y.go:12:3": true,
		"see /w/q/y.go.":                             true,
		"embeds (../q/VERSION)":                      true,
		"reaches ../q/y.go/z/... here":               true,
		"conflicts with x.go:4":                      false,
		"names y.go, which is not in this directory": false,
		"the value is never used":                    false,
		"reaches ../q/z/... here":                    false,
	} {
		finding := Finding{File: "/w/p/x.go", Message: message}
		if got := namesForeignFile(finding, ownFiles(own), known); got != want {
			test.Errorf("%q names another package's file: %t, want %t", message, got, want)
		}
	}
}

// Reading a finding that names no file allocates nothing, since every finding of a package whose reads are recorded is
// read for the files it names, and most name none.
//
// Not parallel: testing.AllocsPerRun refuses to run inside a parallel test, since other tests' allocations would count.
func TestAFindingThatNamesNoFileIsReadWithoutAllocating(test *testing.T) {
	own := &packages.Package{GoFiles: []string{"/w/p/x.go"}}
	known := newKnownFiles()
	known.addPackage(own)
	ownFilesOf := ownFiles(own)
	finding := Finding{File: "/w/p/x.go", Message: "the value (of type int) assigned to x is never used, see the docs"}
	if allocations := testing.AllocsPerRun(100, func() {
		namesForeignFile(finding, ownFilesOf, known)
	}); allocations != 0 {
		test.Errorf("reading a finding that names no file allocated %.0f times, want none", allocations)
	}
}
