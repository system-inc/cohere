package build_dependence

import (
	"go/types"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// graphBytesFixture is the engine's build-facts-chain fixture, one module that imports os, so its load reaches a real
// closure of the standard library, some fifty packages, where every fixture of this package's own reaches none.
var graphBytesFixture = filepath.Join("..", "testdata", "build-facts-chain")

// graphBytesBound is the most the graphs of graphBytesFixture may hold of their own. scratch_graph_cost, which weighs
// the graph the way this test does, read 11.77 MB on main's layout and 2.71 MB on the compact, sealed one, and this
// test reads 2.62 MB, 7.92 MB with Seal doing nothing; 4 MB lets the compact layout grow by half and still pass, and
// fails main's by nearly three times.
const graphBytesBound = 4 << 20

// graphBytesFloor is the least the graphs of graphBytesFixture hold of their own, under half the 2.62 MB this test
// reads: a reading below it saw nothing of them, the graphs still live at the second reading say, and proves nothing.
const graphBytesFloor = 1 << 20

// liveHeapBytes is the heap's live bytes after two full collections, the second sweeping what the first finalized.
func liveHeapBytes() uint64 {
	runtime.GC()
	runtime.GC()
	sample := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

// The graphs' own live bytes stay compact (#gf9qrzf): graphBytesFixture loaded with syntax and types, tests on, its
// standard library on a graph of its own and the rest on the host's, as the engine builds them, every record encoded,
// both graphs sealed and judged and a call index built over them, then the load's syntax and type maps let go as the
// engine's rules let them go. Every package's types are held for the whole test, so what the graphs hold of them is
// no part of their own bytes: the live heap with the graphs, less the live heap once they and every judgment of them
// are let go.
func TestTheGraphsHoldFewBytesOfTheirOwn(test *testing.T) {
	// Not parallel: top-level parallel tests pause until every sequential one has ended, so the heap this test reads
	// holds no other test's allocations.
	roots, loadError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
		Tests: true,
		Dir:   graphBytesFixture,
		Env:   hostBuild.environment,
	}, "./...")
	if loadError != nil {
		test.Fatal(loadError)
	}
	var every []*packages.Package
	packages.Visit(roots, nil, func(each *packages.Package) { every = append(every, each) })
	var typesHeld []*types.Package
	for _, each := range every {
		if len(each.Errors) > 0 {
			test.Fatalf("%s: %v", each.ID, each.Errors)
		}
		if each.Types != nil {
			typesHeld = append(typesHeld, each.Types)
		}
	}
	host, standardLibrary := NewGraph(), NewGraph()
	inStandardLibrary := func(loaded *packages.Package) bool {
		return loaded.Module == nil && loaded.ForTest == "" && !strings.HasSuffix(loaded.PkgPath, ".test")
	}
	graphOfPackage := func(loaded *packages.Package) *Graph {
		if inStandardLibrary(loaded) {
			return standardLibrary
		}
		return host
	}
	standardLibraryPackages := 0
	for _, each := range every {
		if each.TypesInfo == nil {
			continue
		}
		if inStandardLibrary(each) {
			standardLibraryPackages++
		}
		graphOfPackage(each).AddPackage(each, !inStandardLibrary(each))
	}
	if standardLibraryPackages < 20 {
		test.Fatalf("the fixture's load reaches %d packages of the standard library, want its closure of os",
			standardLibraryPackages)
	}
	for _, each := range every {
		if each.TypesInfo == nil {
			continue
		}
		if _, isHeld := graphOfPackage(each).EncodedRecord(each.ID); !isHeld {
			test.Fatalf("%s: its graph holds no record of it", each.ID)
		}
	}
	host.Seal()
	standardLibrary.Seal()
	hostJudged, standardLibraryJudged := host.Against(Difference{}), standardLibrary.Against(Difference{})
	calls := NewCallIndex(host, standardLibrary)
	units := calls.UnitCount()
	// The load itself is let go here too: neither roots nor every is read again, so neither is live at the readings.
	for _, each := range every {
		each.Syntax, each.TypesInfo = nil, nil
	}

	withGraphs := liveHeapBytes()
	runtime.KeepAlive(host)
	runtime.KeepAlive(standardLibrary)
	runtime.KeepAlive(hostJudged)
	runtime.KeepAlive(standardLibraryJudged)
	runtime.KeepAlive(calls)
	withoutGraphs := liveHeapBytes()
	runtime.KeepAlive(typesHeld)

	ownBytes := int64(withGraphs) - int64(withoutGraphs)
	test.Logf("%d units, %d packages of the standard library: the graphs hold %.2f MB of their own, the bound %.2f MB",
		units, standardLibraryPackages, float64(ownBytes)/(1<<20), float64(graphBytesBound)/(1<<20))
	if ownBytes > graphBytesBound {
		test.Errorf("the graphs hold %.2f MB of their own, over the bound of %.2f MB", float64(ownBytes)/(1<<20),
			float64(graphBytesBound)/(1<<20))
	}
	if ownBytes < graphBytesFloor {
		test.Errorf("the graphs hold %.2f MB of their own, under %.2f MB: the reading saw nothing of them, so it "+
			"proves nothing", float64(ownBytes)/(1<<20), float64(graphBytesFloor)/(1<<20))
	}
}
