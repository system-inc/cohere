package check

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"testing"
	"weak"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/export_data"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// A kept package's export data reader holds the package's types and nothing else of the host's analysis: its syntax
// and type maps are collected while the reader lives, and the reader still writes the types, once however many loads
// read at the same moment, as export data that imports back to the same package.
//
// Not parallel: whether the type maps were collected is read after runtime.GC, which another test's allocations could
// keep from finishing what this one asks.
func TestKeptExportDataHoldsOnlyThePackagesTypes(test *testing.T) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "kept.go", "package kept\n\ntype Counter struct{ count int }\n\n"+
		"func (counter *Counter) Add() { counter.count++ }\n", parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}
	checked, checkError := (&types.Config{Importer: importer.Default()}).Check("example.com/kept", fileSet,
		[]*ast.File{file}, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	kept := &packages.Package{ID: "example.com/kept", PkgPath: "example.com/kept", Types: checked, Fset: fileSet,
		Syntax: []*ast.File{file}, TypesInfo: information}
	typeMaps := weak.Make(information)

	readExportData := lazyExportData(kept)
	kept, information, file = nil, nil, nil
	runtime.GC()
	if typeMaps.Value() != nil {
		test.Error("the export data reader kept the package's type maps alive, want only its types held")
	}

	written := map[string]bool{}
	var lock sync.Mutex
	var reading sync.WaitGroup
	for range 16 {
		reading.Go(func() {
			exportData, readError := readExportData()
			lock.Lock()
			defer lock.Unlock()
			written[string(exportData)] = true
			if readError != nil {
				test.Errorf("a load read the export data with %v, want none", readError)
			}
		})
	}
	reading.Wait()
	if len(written) != 1 {
		test.Fatalf("16 loads read %d different export data, want one write they all read", len(written))
	}
	for exportData := range written {
		// The package imports nothing, so it reads against no other package.
		imported, importError := export_data.Read(token.NewFileSet(), map[string]*types.Package{}, []byte(exportData),
			"example.com/kept")
		if importError != nil || imported.Scope().Lookup("Counter") == nil {
			test.Errorf("the export data imported as %v with %v, want the package with Counter", imported, importError)
		}
	}
}

// Release lets go of every entry and rendering the host kept once no build is left to ask, and keeps the count the
// record writes; a run that kept nothing releases nothing.
func TestReleasingTheHostKeepsOnlyItsCounts(test *testing.T) {
	test.Parallel()

	host := &HostRun{entries: map[string]*hostEntry{"a": {}, "b": {}}, renderings: NewReadsBuild(), kept: 2}
	host.Release()
	if host.entries != nil || host.renderings != nil {
		test.Errorf("after Release the host holds %d entries and renderings %v, want none", len(host.entries),
			host.renderings)
	}
	if host.EntriesKept() != 2 {
		test.Errorf("after Release the host counts %d entries kept, want 2", host.EntriesKept())
	}
	var none *HostRun
	none.Release()
}

// Each entry the host kept is decoded once a run, however many builds consider it (#954dadc): two windows builds, each
// with a store of its own, both restore base and user from the host, and each entry they considered was decoded once,
// by whichever build came first.
func TestEachKeptEntryIsDecodedOnceHoweverManyBuildsConsiderIt(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	host := throughHost(test, root, filepath.Join(stores, "host"))
	for _, store := range []string{"first", "second"} {
		built, _ := throughBuild(test, root, filepath.Join(stores, store), host)
		considered := append(slices.Clone(built.FromHost.Restored), built.FromHost.Untouched...)
		for _, path := range []string{throughBasePath, throughUserPath} {
			if !slices.Contains(considered, path) {
				test.Fatalf("the %s build restored %q and left %q untouched, want %s among them", store,
					built.FromHost.Restored, built.FromHost.Untouched, path)
			}
		}
	}
	for _, path := range []string{throughBasePath, throughUserPath} {
		if decodes := host.entries[path].decodes.Load(); decodes != 1 {
			test.Errorf("%s: two builds considered its entry and it was decoded %d times, want once", path, decodes)
		}
	}
	for identifier, kept := range host.entries {
		if decodes := kept.decodes.Load(); decodes > 1 {
			test.Errorf("%s: its entry was decoded %d times, want at most once", identifier, decodes)
		}
	}
}

// What two builds share of an entry's decode is what neither writes: each has a package and types of its own, whose
// Holds, Declined and Untouched its restore sets, over the one decode's analyses, and that decode is the one a build
// makes of the entry by its own analyzers, which, made from the same rules, decode alike.
func TestTwoBuildsShareAnEntrysDecodeButNotItsTypes(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	host := throughHost(test, root, filepath.Join(test.TempDir(), "host"))
	kept := host.entries[throughUserPath]
	shared, decodeError := kept.decoded()
	if decodeError != nil {
		test.Fatalf("decoding user's entry: %v", decodeError)
	}
	first, second := restoredForBuild(shared), restoredForBuild(shared)
	if first == second || first.types == second.types || first.types == shared.types {
		test.Fatal("two builds were given one package or one types of the shared decode, which each build writes")
	}
	first.types.Untouched = true
	first.types.Holds = func(map[string]*types.Package) bool { return true }
	if second.types.Untouched || second.types.Holds != nil || shared.types.Untouched || shared.types.Holds != nil {
		test.Error("one build's restore wrote the other's types, or the shared decode's")
	}
	if reflect.ValueOf(first.analyses).UnsafePointer() != reflect.ValueOf(shared.analyses).UnsafePointer() {
		test.Error("a build was given a copy of the shared decode's analyses, want the one decode")
	}

	buildSession := NewCacheSession(cachingAt(test.TempDir(), throughRules()), throughRules())
	if buildSession.known == host.known || !decodesAlike(buildSession.known, host.known) {
		test.Fatal("a build's session made from the host's rules decodes unlike the host's, so nothing is shared")
	}
	body, record, readExportData, readError := kept.contents()
	if readError != nil {
		test.Fatalf("reading user's entry: %v", readError)
	}
	own, ownError := decodeEntry(buildSession.known, &packages.Package{ID: throughUserPath}, body, record,
		readExportData, false)
	if ownError != nil {
		test.Fatalf("decoding user's entry by the build's analyzers: %v", ownError)
	}
	if !reflect.DeepEqual(comparableRestored(own), comparableRestored(shared)) {
		test.Errorf("the build's own decode\n%+v\nthe shared decode\n%+v", comparableRestored(own),
			comparableRestored(shared))
	}
}

// A build whose analyzers would decode an entry otherwise than the host's never shares the host's decode: another set
// of rules holds other slots.
func TestAnalyzersOfOtherRulesDecodeUnlike(test *testing.T) {
	test.Parallel()

	hostSession := NewCacheSession(cachingAt(test.TempDir(), throughRules()), throughRules())
	fewer := []rule.Rule{throughRules()[0]}
	fewerSession := NewCacheSession(cachingAt(test.TempDir(), fewer), fewer)
	if decodesAlike(hostSession.known, fewerSession.known) || decodesAlike(nil, hostSession.known) {
		test.Error("analyzers of other rules, or none, decode alike with the host's, want unlike")
	}
}

// Release lets go of each entry's shared decode with the entry: nothing of the host's run holds it after.
//
// Not parallel: whether the decode was collected is read after runtime.GC, which another test's allocations could keep
// from finishing what this one asks.
func TestReleasingTheHostLetsGoOfItsSharedDecodes(test *testing.T) {
	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	host := throughHost(test, root, filepath.Join(test.TempDir(), "host"))
	shared, decodeError := host.entries[throughUserPath].decoded()
	if decodeError != nil {
		test.Fatalf("decoding user's entry: %v", decodeError)
	}
	decoded := weak.Make(shared)
	shared = nil
	host.Release()
	runtime.GC()
	if decoded.Value() != nil {
		test.Error("after Release the host's run still holds user's shared decode")
	}
	if host.known != nil {
		test.Error("after Release the host holds the analyzers its decodes read, want none")
	}
}
