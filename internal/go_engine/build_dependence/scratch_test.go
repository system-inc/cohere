package build_dependence

import (
	"fmt"
	"go/ast"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"unsafe"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/needs"
)

// scratchFixtures are the fixtures whose packages the scratch's tests add, every one block 1's tests load.
var scratchFixtures = []string{"layout", "methods", "lowering"}

// chainLoad is the go_engine's build-facts-chain fixture loaded whole under the host: its package and the 51 of the
// standard library it reaches through os, runtime, reflect and syscall among them, which give a scratch packages of
// every size one after another, where block 1's fixtures are four small packages that import nothing.
var chainLoad = sync.OnceValues(func() (loadedBuild, error) {
	roots, loadError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
		Tests: true,
		Dir:   filepath.Join("..", "testdata", "build-facts-chain"),
		Env:   hostBuild.environment,
	}, "./...")
	if loadError != nil {
		return nil, loadError
	}
	loaded := loadedBuild{}
	packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.ID] = visited })
	for _, visited := range loaded {
		if len(visited.Errors) > 0 {
			return nil, fmt.Errorf("%s: %v", visited.ID, visited.Errors)
		}
	}
	return loaded, nil
})

// scratchHosts are the loads the scratch's tests add, by name: block 1's fixtures under the host, and the chain
// fixture with the standard library it reaches (chainLoad).
func scratchHosts(test *testing.T) map[string]loadedBuild {
	test.Helper()
	hosts := map[string]loadedBuild{}
	for _, fixture := range scratchFixtures {
		hosts[fixture] = loadFixture(test, fixture, hostBuild)
	}
	chain, loadError := chainLoad()
	if loadError != nil {
		test.Fatal(loadError)
	}
	hosts["chain"] = chain
	return hosts
}

// sortedPackages are host's packages in the order of their IDs.
func sortedPackages(host loadedBuild) []*packages.Package {
	var sorted []*packages.Package
	for _, identifier := range slices.Sorted(maps.Keys(host)) {
		sorted = append(sorted, host[identifier])
	}
	return sorted
}

// encodedRecords are the records of every package graph holds, encoded, by package ID.
func encodedRecords(graph *Graph) map[string][]byte {
	records := map[string][]byte{}
	for _, identifier := range slices.Sorted(maps.Keys(graph.packages)) {
		records[identifier], _ = graph.EncodedRecord(identifier)
	}
	return records
}

// freshRecords are the records of host's packages, each added through a scratch of its own, as if no scratch were
// ever reused.
func freshRecords(host loadedBuild) map[string][]byte {
	graph := NewGraph()
	for _, loaded := range sortedPackages(host) {
		graph.addPackage(loaded, fixtureWorkspace(loaded), nil, newWorkerScratch())
	}
	return encodedRecords(graph)
}

// addedThrough is loaded's record, added to a graph of its own through scratch, which it releases after, as
// AddPackage does. A walk misled by what the last package left, a namer's index past its table say, can panic; that
// is reported as the failure it is.
func addedThrough(test *testing.T, loaded *packages.Package, scratch *workerScratch) *packageRecord {
	test.Helper()
	graph := NewGraph()
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				test.Errorf("%s: adding it through a used scratch panicked: %v", loaded.ID, recovered)
			}
		}()
		graph.addPackage(loaded, fixtureWorkspace(loaded), nil, scratch)
	}()
	scratch.release()
	return graph.packages[loaded.ID]
}

// expectRecord fails when record is not, byte for byte, the record want encodes.
func expectRecord(test *testing.T, label string, identifier string, record *packageRecord, want []byte) {
	test.Helper()
	if record == nil {
		test.Errorf("%s: %s has no record", label, identifier)
		return
	}
	if encoded := encodeRecord(record); string(encoded) != string(want) {
		test.Errorf("%s: %s's record is %d bytes unlike the %d of one added through a fresh scratch", label,
			identifier, len(encoded), len(want))
	}
}

// One scratch reused across every package of block 1's fixtures and of the standard library the chain fixture
// reaches, in the order of their IDs and again backwards, so each package follows another in each pass and the loads
// follow one another, writes every record byte for byte as a scratch of its own for each package does (#86qvr6b).
func TestAReusedScratchWritesTheRecordsAFreshOneDoes(test *testing.T) {
	test.Parallel()

	hosts := scratchHosts(test)
	scratch := newWorkerScratch()
	records := 0
	for _, backwards := range []bool{false, true} {
		for _, name := range slices.Sorted(maps.Keys(hosts)) {
			host := hosts[name]
			fresh := freshRecords(host)
			ordered := sortedPackages(host)
			if backwards {
				slices.Reverse(ordered)
			}
			for _, loaded := range ordered {
				expectRecord(test, name, loaded.ID, addedThrough(test, loaded, scratch), fresh[loaded.ID])
				records++
			}
		}
	}
	// Block 1's fixtures hold four packages and the chain fixture's load 52.
	if records < 2*50 {
		test.Fatalf("only %d records were compared, so the scratch was reused across few packages", records)
	}
}

// A scratch carries nothing of one package into the next: each package of the layout fixture added, then the next
// one, then the first again through the same scratch, writes the record a fresh scratch writes. The first package's
// own objects are what the second add of it meets, so a map the scratch failed to clear would mislead it: an edge
// left in edgeSeen drops the edge it marks, and an index left in the namer's indexes names an object the new record's
// table never got. Leave either uncleared in release and this fails.
func TestAScratchCarriesNothingOfOnePackageIntoTheNext(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "layout", hostBuild)
	fresh := freshRecords(host)
	ordered := sortedPackages(host)
	scratch := newWorkerScratch()
	edges, objects := 0, 0
	for index, loaded := range ordered {
		first := addedThrough(test, loaded, scratch)
		addedThrough(test, ordered[(index+1)%len(ordered)], scratch)
		again := addedThrough(test, loaded, scratch)
		expectRecord(test, "added first", loaded.ID, first, fresh[loaded.ID])
		expectRecord(test, "added again", loaded.ID, again, fresh[loaded.ID])
		if first != nil {
			edges += len(first.edges)
			objects += len(first.objects)
		}
	}
	if edges == 0 || objects == 0 {
		test.Fatalf("the layout fixture's records hold %d edges and %d objects, so a leftover could change nothing",
			edges, objects)
	}
}

// Packages added at once from many goroutines, each load's whole host into four graphs side by side, every add taking
// a scratch from the shared pool and giving it back, write the records of a serial build with a fresh scratch for
// each package. Under -race this is the scratch's concurrency check: a scratch two adds held at once would race.
func TestPackagesAddedAtOnceWriteTheRecordsOfASerialBuild(test *testing.T) {
	test.Parallel()

	hosts := scratchHosts(test)
	for _, name := range slices.Sorted(maps.Keys(hosts)) {
		host := hosts[name]
		serial := freshRecords(host)
		graphs := make([]*Graph, 4)
		var group sync.WaitGroup
		for index := range graphs {
			group.Go(func() { graphs[index] = graphOf(host, nil) })
		}
		group.Wait()
		for index, graph := range graphs {
			records := encodedRecords(graph)
			if len(records) != len(serial) {
				test.Errorf("%s: graph %d holds %d records, the serial build %d", name, index, len(records),
					len(serial))
			}
			for identifier, want := range serial {
				if string(records[identifier]) != string(want) {
					test.Errorf("%s: graph %d's record of %s is %d bytes unlike the serial build's %d", name,
						index, identifier, len(records[identifier]), len(want))
				}
			}
		}
	}
}

// newUnit's one walk of a declaration sizes it as countNodes does and finds the universe's names a walk of its own
// finds, for every unit of block 1's fixtures and of the standard library the chain fixture reaches, so folding the
// two walks into one changed no unit's size or names.
func TestAUnitsOneWalkSizesItAndFindsItsUniverseNames(test *testing.T) {
	test.Parallel()

	units := 0
	for _, host := range scratchHosts(test) {
		for _, loaded := range sortedPackages(host) {
			collecting := newWorkerScratch().collector(loaded.TypesInfo)
			for _, file := range loaded.Syntax {
				tokenFile := loaded.Fset.File(file.FileStart)
				for _, declaration := range unitDeclarations(file) {
					var universeNames uint64
					ast.Inspect(declaration, func(node ast.Node) bool {
						if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
							object := loaded.TypesInfo.Uses[identifier]
							if object != nil && object.Parent() == types.Universe {
								universeNames |= universeNameBit(object.Name())
							}
						}
						return true
					})
					unit := collecting.newUnit(tokenFile, declaration)
					if unit.nodes != countNodes(declaration) || unit.universeNames != universeNames {
						test.Errorf("%s %s:%d: the walk finds %d nodes and names %x, want %d and %x", loaded.ID,
							unit.file, unit.start, unit.nodes, unit.universeNames, countNodes(declaration),
							universeNames)
					}
					units++
				}
			}
		}
	}
	if units == 0 {
		test.Fatal("the fixtures hold no unit, so the walk was compared with nothing")
	}
}

// Inside one package a unit's callees and the owners of the members its header declares are its own, though the
// scratch keeps the callees seen and the owners from one unit to the next: in the resets fixture First and Second both
// call Shared and each carries it, and Loose and Speaker, value specs right after the types Holder and Plain, own the
// members their anonymous types declare while neither type gains an edge to them. The reuse tests above compare one
// scratch with another, both wrong alike should a reset inside a package go, so this asks the record itself. Leave the
// callees seen uncleared before a unit and Second carries none; leave the owners uncut before a value spec and Holder
// gains an edge to Field.
func TestAUnitsCalleesAndOwnersAreItsOwnInsideOnePackage(test *testing.T) {
	test.Parallel()

	host := loadFixture(test, "resets", hostBuild)
	record := graphOf(host, nil).packages["example.com/resets"]
	if record == nil {
		test.Fatal("the graph of the resets fixture holds no record of example.com/resets")
	}
	callees := map[string][]string{}
	for _, unit := range record.units {
		for _, callee := range unit.callees {
			callees[unit.declares[0].local] = append(callees[unit.declares[0].local],
				record.objects[callee].object.Name())
		}
	}
	for _, caller := range []string{"First", "Second"} {
		if !slices.Equal(callees[caller], []string{"Shared"}) {
			test.Errorf("%s: want the callees [Shared], got %q", caller, callees[caller])
		}
	}
	edges := map[string]bool{}
	for _, edge := range record.edges {
		edges[edge.from+" to "+record.objects[edge.to].object.Name()] = true
	}
	for _, edge := range []struct {
		name string
		held bool
	}{
		{"Holder to Count", true},
		{"Loose to Field", true},
		{"Speaker to Speak", true},
		{"Holder to Field", false},
		{"Plain to Speak", false},
	} {
		if edges[edge.name] != edge.held {
			test.Errorf("the edge %s: want held %t, got %t", edge.name, edge.held, edges[edge.name])
		}
	}
}

// scratchOwnPointers are the pointers a released scratch keeps, by their path in it, each at a part of the scratch
// itself, so they hold nothing of any package: the body walk's and the collector's bit walker, and the collector's
// body walk (newWorkerScratch).
var scratchOwnPointers = map[string]bool{"bodies.walker": true, "collecting.walker": true, "collecting.bodies": true}

// poisoned sets every number under value that is zero to one, and every flag to true, so a reset release leaves out
// shows whatever the package left it.
//
// Mutates: value has its numbers and flags set in place, the scratch's own fields
func poisoned(value reflect.Value) {
	switch {
	case value.Kind() == reflect.Struct:
		for index := range value.NumField() {
			poisoned(value.Field(index))
		}
	case value.CanInt() && value.IsZero():
		settable(value).SetInt(1)
	case value.CanUint() && value.IsZero():
		settable(value).SetUint(1)
	case value.Kind() == reflect.Bool:
		settable(value).SetBool(true)
	}
}

// settable is value, an addressable field reflection reads but will not set since it is unexported, as a value it
// sets.
//
// Unsafe: reflect.NewAt takes the field's own address with the field's own type, so the value it makes is the field
// itself, written in place, and the scratch it lies in outlives the write.
func settable(value reflect.Value) reflect.Value {
	return reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem()
}

// expectEmptied fails for each part of value, at path in a released scratch, that holds anything: a map with an entry,
// a slice with a length or with an element up to its capacity that is not zero, and a pointer, number, flag or any
// other value that is not zero, but for the scratch's own pointers (scratchOwnPointers).
func expectEmptied(test *testing.T, path string, value reflect.Value) {
	test.Helper()
	switch value.Kind() {
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Type().Field(index).Name
			if path != "" {
				field = path + "." + field
			}
			if !scratchOwnPointers[field] {
				expectEmptied(test, field, value.Field(index))
			}
		}
	case reflect.Map:
		if value.Len() > 0 {
			test.Errorf("%s holds %d entries, want none", path, value.Len())
		}
	case reflect.Slice:
		if value.Len() > 0 {
			test.Errorf("%s holds %d elements, want none", path, value.Len())
		}
		room := value.Slice3(0, value.Cap(), value.Cap())
		for index := range room.Len() {
			if !room.Index(index).IsZero() {
				test.Errorf("%s holds an element at %d of its room of %d, want every one zero", path, index,
					room.Len())
				break
			}
		}
	default:
		if !value.IsZero() {
			test.Errorf("%s is %v, want zero", path, value)
		}
	}
}

// A released scratch holds nothing of the package it last added (release): after the chain load's package of the most
// declarations, every map is empty, every slice cut to nothing with each element up to its capacity zero, and every
// pointer, number and flag zero, but for the pointers between the scratch's own parts, which still point there. Each
// number and flag is set first where the package left it zero (poisoned), so a reset release leaves out shows however
// the package left its field, and a field added to the scratch later without a reset fails here too.
func TestAReleasedScratchHoldsNothingOfThePackageItAdded(test *testing.T) {
	test.Parallel()

	chain, loadError := chainLoad()
	if loadError != nil {
		test.Fatal(loadError)
	}
	var largest *packages.Package
	most := 0
	for _, loaded := range sortedPackages(chain) {
		declarations := 0
		for _, file := range loaded.Syntax {
			declarations += len(file.Decls)
		}
		if declarations > most {
			largest, most = loaded, declarations
		}
	}
	if largest == nil {
		test.Fatal("the chain fixture's load holds no package with a declaration")
	}
	scratch := newWorkerScratch()
	NewGraph().addPackage(largest, fixtureWorkspace(largest), nil, scratch)
	if len(scratch.collecting.units) == 0 || len(scratch.naming.objects) == 0 ||
		len(scratch.bodies.typeReferences) == 0 {
		test.Fatalf("%s left %d units, %d named objects and %d walked types in the scratch, so emptying it shows "+
			"nothing", largest.ID, len(scratch.collecting.units), len(scratch.naming.objects),
			len(scratch.bodies.typeReferences))
	}
	poisoned(reflect.ValueOf(scratch).Elem())
	scratch.release()
	if scratch.bodies.walker != &scratch.walker || scratch.collecting.walker != &scratch.walker ||
		scratch.collecting.bodies != &scratch.bodies {
		test.Error("a released scratch's walk no longer points at its own parts")
	}
	expectEmptied(test, "", reflect.ValueOf(scratch).Elem())
}

// reusedAddAllocations bounds the objects adding the layout fixture's package allocates when its scratch is a reused
// one. Measured on darwin with Go 1.27 (#86qvr6b): 74 an add through a reused scratch, 169 with no scratch given back
// to the pool, so each add made its own; the bound sits between, clear of either.
const reusedAddAllocations = 120

// Adding a package again takes the scratch the last add gave back rather than growing one anew, so it allocates the
// record and little else (#86qvr6b). Stop giving scratches back to the pool and every add grows a scratch, which this
// counts. A collection between two adds moves the pool's scratch to its victim cache, where the next add still finds
// it, and starts only at an allocation, which nothing between giving a scratch back and taking it makes; and the
// count is an average over many adds, so an add that misses the pool now and then stays under the bound.
//
// Not parallel: testing.AllocsPerRun counts every goroutine's allocations, so a test running beside it would add its
// own to the count.
func TestAddingAPackageAgainReusesAScratch(test *testing.T) {
	needs.NoRace(test, "the race detector makes sync.Pool drop a quarter of what is put into it at random, so a "+
		"scratch given back is often not there to take")
	loaded := loadFixture(test, "layout", hostBuild)["example.com/layout"]
	if loaded == nil {
		test.Fatal("the layout fixture's load holds no package example.com/layout")
	}
	graph := NewGraph()
	allocations := testing.AllocsPerRun(100, func() { graph.AddPackage(loaded, true, nil) })
	if allocations > reusedAddAllocations {
		test.Errorf("adding example.com/layout again allocates %.0f objects, more than the %d of an add through a "+
			"reused scratch, so the add grew a scratch of its own", allocations, reusedAddAllocations)
	}
}
