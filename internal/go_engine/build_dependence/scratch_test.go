package build_dependence

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
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
		graph.addPackage(loaded, fixtureWorkspace(loaded), newWorkerScratch())
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
		graph.addPackage(loaded, fixtureWorkspace(loaded), scratch)
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
