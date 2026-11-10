package build_reads

import (
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
	// Two lists of the same type arguments, each its own instantiation, which the reads keep as one instance.
	box := types.NewNamed(types.NewTypeName(token.NoPos, other, "Box", nil), types.Typ[types.Int], nil)
	box.SetTypeParams([]*types.TypeParam{
		types.NewTypeParam(types.NewTypeName(token.NoPos, other, "Type", nil), types.NewInterfaceType(nil, nil)),
	})
	var lists []*types.TypeList
	for range 2 {
		instance, instantiateError := types.Instantiate(nil, box, []types.Type{types.Typ[types.Int]}, true)
		named, isNamed := instance.(*types.Named)
		if instantiateError != nil || !isNamed {
			test.Fatalf("instantiating Box[int]: %v", instantiateError)
		}
		lists = append(lists, named.TypeArgs())
	}
	if lists[0] == lists[1] {
		test.Fatal("the two instantiations share their type arguments, so nothing tells identical lists apart")
	}
	reads := &Reads{}
	var group sync.WaitGroup
	for round := range 8 {
		group.Go(func() {
			for _, object := range objects {
				reads.AddObject(object)
				reads.AddFact(Fact{Object: object, Package: object.Pkg(), FactType: factType, Found: true})
			}
			reads.AddFact(Fact{Package: other, FactType: factType})
			reads.AddInstance(box.Obj(), lists[round%2])
		})
	}
	group.Wait()
	reads.MarkNever("first")
	reads.MarkNever("second")

	var names []string
	for _, object := range reads.Objects() {
		names = append(names, object.Pkg().Path()+"."+object.Name())
	}
	want := []string{"example.com/other.Beta", "example.com/other.Zeta", "example.com/second.Alpha"}
	if !slices.Equal(names, want) {
		test.Fatalf("want the objects\n%q\ngot\n%q", want, names)
	}
	facts := reads.Facts()
	if len(facts) != 4 || facts[0].Object != nil || facts[0].Package != other {
		test.Fatalf("want three object facts after other's package fact, got %+v", facts)
	}
	if instances := reads.Instances(); len(instances) != 1 {
		test.Fatalf("want the one instance, identical type arguments kept once, got %d", len(instances))
	}
	if never := reads.Never(); never != "first" {
		test.Fatalf("want the first reason kept, got %q", never)
	}
}
