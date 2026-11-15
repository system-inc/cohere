package unused_code

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"testing"
)

// storageMethods is how many objects TestGraphIsSizedOnceAndWordsNoObjectWhileBuilding builds a graph of, enough
// that a node list or a map starting empty would grow many times over.
const storageMethods = 4096

// TestGraphIsSizedOnceAndWordsNoObjectWhileBuilding holds the graph's storage to its sizes (#k8e189q): a node is the
// object it stands for and its edges, 64 bytes, an edge map is keyed by one word, and filling a graph sized from its
// package's declarations allocates only each node's own lists of uses and ownerships. A node list or a map starting
// empty would allocate again as it grew, and a node worded as it is made would allocate for each method's name. The
// verdict's lists are each made once at their size.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestGraphIsSizedOnceAndWordsNoObjectWhileBuilding(test *testing.T) {
	if size := reflect.TypeFor[node]().Size(); size != 64 {
		test.Errorf("a node is %d bytes, not 64: it holds more than its object and its edges", size)
	}
	// Each method is owned and used by the method before it, so filling the graph gives every method but the last one
	// list of ownerships and one of uses, a single entry each: two allocations a method, and no other.
	expected := float64(2 * (storageMethods - 1))
	const runs = 4
	graphs := make([]storageGraph, runs+1)
	for index := range graphs {
		graphs[index] = newStorageGraph()
	}
	for _, edges := range []reflect.Type{
		reflect.TypeOf(graphs[0].graph.usedEdges), reflect.TypeOf(graphs[0].graph.ownedEdges),
	} {
		if size := edges.Key().Size(); size != 8 {
			test.Errorf("an edge map's key is %d bytes, not one word", size)
		}
	}
	filled := 0
	allocations := testing.AllocsPerRun(runs, func() {
		graphs[filled].fill()
		filled++
	})
	if allocations != expected {
		test.Errorf("filling a graph of %d methods allocated %.0f times, not %.0f: the node list or a map grew, or a "+
			"node was worded as it was made", storageMethods, allocations, expected)
	}

	verdict := graphs[0].graph.results()
	if len(verdict.used)+len(verdict.unused)+len(verdict.quiet) != storageMethods {
		test.Fatalf("the verdict names %d objects, not %d", len(verdict.used)+len(verdict.unused)+len(verdict.quiet),
			storageMethods)
	}
	lists := map[string][]object{"used": verdict.used, "unused": verdict.unused, "quiet": verdict.quiet}
	for name, list := range lists {
		if cap(list) != len(list) {
			test.Errorf("the verdict's %s list holds %d in room for %d: it grew rather than being made at its size",
				name, len(list), cap(list))
		}
	}
}

// storageGraph is a graph of a package declaring storageMethods methods of one type, each named in its
// declarations as type-checking names them, and the methods in the order fill makes them nodes.
type storageGraph struct {
	graph   *graph
	methods []types.Object
}

// newStorageGraph is a storageGraph none of whose methods is yet a node.
func newStorageGraph() storageGraph {
	typesPackage := types.NewPackage("example.com/storage", "storage")
	typeName := types.NewTypeName(token.NoPos, typesPackage, "Storage", nil)
	named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)
	typesInfo := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	methods := make([]types.Object, 0, storageMethods)
	for index := range storageMethods {
		receiver := types.NewParam(token.NoPos, typesPackage, "storage", types.NewPointer(named))
		signature := types.NewSignatureType(receiver, nil, nil, nil, nil, false)
		name := fmt.Sprintf("method%d", index)
		method := types.NewFunc(token.NoPos, typesPackage, name, signature)
		typesInfo.Defs[ast.NewIdent(name)] = method
		methods = append(methods, method)
	}
	return storageGraph{
		graph:   newGraph(token.NewFileSet(), nil, typesPackage, typesInfo, nil, nil),
		methods: methods,
	}
}

// fill makes each method a node, owned and used by the method before it.
func (storage storageGraph) fill() {
	for index := 1; index < len(storage.methods); index++ {
		storage.graph.see(storage.methods[index], storage.methods[index-1])
		storage.graph.use(storage.methods[index], storage.methods[index-1])
	}
}
