package build_reads

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"sync"
	"testing"
)

// readFact is a fact type for the tests.
type readFact struct{}

// AFact makes readFact an analysis.Fact.
func (*readFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// Reads from many goroutines at once, as the actions on one package add them, keep each read once, in an order that
// does not depend on the order they came in, and the first reason the package is never restored.
func TestReadsFromManyGoroutinesKeepEachReadOnceInOrder(test *testing.T) {
	test.Parallel()

	other := types.NewPackage("example.com/other", "other")
	second := types.NewPackage("example.com/second", "second")
	objects := []types.Object{
		types.NewFunc(token.NoPos, second, "Alpha", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewVar(token.NoPos, other, "Zeta", types.Typ[types.Int]),
		types.NewConst(token.NoPos, other, "Beta", types.Typ[types.Int], nil),
	}
	factType := reflect.TypeFor[*readFact]()
	reads := &Reads{}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for _, object := range objects {
				reads.AddUse(object, UseMember)
				reads.AddFact(Fact{Object: object, Package: object.Pkg(), FactType: factType, Found: true})
			}
			reads.AddFact(Fact{Package: other, FactType: factType})
		})
	}
	group.Wait()
	reads.MarkNever("first")
	reads.MarkNever("second")

	var names []string
	for _, use := range reads.Uses() {
		names = append(names, use.Object.Pkg().Path()+"."+use.Object.Name()+" "+string(use.Kind))
	}
	want := []string{
		"example.com/other.Beta Member",
		"example.com/other.Zeta Member",
		"example.com/second.Alpha Member",
	}
	if !slices.Equal(names, want) {
		test.Fatalf("want the uses\n%q\ngot\n%q", want, names)
	}
	facts := reads.Facts()
	if len(facts) != 4 || facts[0].Object != nil || facts[0].Package != other {
		test.Fatalf("want three object facts after other's package fact, got %+v", facts)
	}
	if never := reads.Never(); never != "first" {
		test.Fatalf("want the first reason kept, got %q", never)
	}
}

// Reads are restorable only once their type reads are recorded and while no never mark is set: reads never recorded
// are unknown, which is never read as empty, however little else they hold.
func TestUnknownReadsAreNeverRestorable(test *testing.T) {
	test.Parallel()

	unknown := &Reads{}
	if restorable, why := unknown.Restorable(); restorable || why != "its type reads were never recorded" {
		test.Fatalf("want unknown reads unrestorable, never recorded, got %t, %q", restorable, why)
	}
	recorded := &Reads{}
	recorded.RecordTypes(types.NewPackage("example.com/own", "own"), &types.Info{}, nil)
	if restorable, why := recorded.Restorable(); !restorable || !recorded.Recorded() {
		test.Fatalf("want recorded empty reads restorable, got %t, %q", restorable, why)
	}
	recorded.MarkNever(RestoredFromCache)
	if restorable, why := recorded.Restorable(); restorable || why != RestoredFromCache {
		test.Fatalf("want reads marked never unrestorable for the mark, got %t, %q", restorable, why)
	}
}

// Sorting a package's selections through one receiver, and its uses of one object in several kinds, compares that
// object with itself and writes nothing: rendering a large struct twice for each such comparison doubled every extra
// build's decide on deeafbf4 (#p1s9gdk). Sorting two hundred selections through a struct of two hundred fields, and two
// hundred objects each used in two kinds, allocates the result's slice and nothing per comparison.
//
// Not parallel: testing.AllocsPerRun counts the whole process's allocations, so another test running beside it would
// be counted too.
func TestSortingReadsOfOneObjectWritesNothing(test *testing.T) {
	other := types.NewPackage("example.com/other", "other")
	fields := make([]*types.Var, 0, 200)
	for index := range 200 {
		fields = append(fields, types.NewField(token.NoPos, other, fmt.Sprintf("Field%d", index), types.Typ[types.Int],
			false))
	}
	receiver := types.NewTypeName(token.NoPos, other, "Wide", nil)
	types.NewNamed(receiver, types.NewStruct(fields, nil), nil)
	reads := &Reads{}
	collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[string]Selection{}}
	for index, field := range fields {
		selection := Selection{Receiver: receiver, Name: field.Name(), Index: []int{index}, Selected: field}
		collected.selections[selectionKey(selection)] = selection
		collected.uses[ObjectUse{Object: field, Kind: UseMember}] = true
		collected.uses[ObjectUse{Object: field, Kind: UseWhole}] = true
	}
	reads.publishTypes(collected)

	if allocations := testing.AllocsPerRun(5, func() { reads.Selections() }); allocations > 1 {
		test.Errorf("sorting %d selections through one receiver allocated %.0f times, want the result's slice alone",
			len(fields), allocations)
	}
	if allocations := testing.AllocsPerRun(5, func() { reads.Uses() }); allocations > 1 {
		test.Errorf("sorting %d objects each used in two kinds allocated %.0f times, want the result's slice alone",
			len(fields), allocations)
	}
}
