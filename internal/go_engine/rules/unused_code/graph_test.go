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

// TestGraphIsSizedOnceAndWordsNoObjectWhileBuilding holds the graph's storage to its sizes (#k8e189q, #fex6s0v): a
// node is the object it stands for and where its two chains of edges start, 24 bytes, a link of a chain is two 32-bit
// indexes, the object map and the edge maps are keyed by one word, and filling a graph sized from its package's
// declarations allocates nothing. Lists of uses and ownerships kept on each node would allocate for every node with
// an edge, a node list, a list of links or a map starting empty would allocate again as it grew, and a node worded as
// it is made would allocate for each method's name. The verdict's lists are each made once at their size.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestGraphIsSizedOnceAndWordsNoObjectWhileBuilding(test *testing.T) {
	if size := reflect.TypeFor[node]().Size(); size != 24 {
		test.Errorf("a node is %d bytes, not 24: it holds more than its object and where its edges start", size)
	}
	if size := reflect.TypeFor[edgeLink]().Size(); size != 8 {
		test.Errorf("a link is %d bytes, not 8: its indexes are wider than 32 bits", size)
	}
	// Each method is owned and used by the method before it, so filling the graph makes every method a node and links
	// an ownership and a use for each, all in room the graph was made with.
	const expected = 0
	const runs = 4
	graphs := make([]storageGraph, runs+1)
	for index := range graphs {
		graphs[index] = newStorageGraph()
	}
	for _, keyed := range []reflect.Type{
		reflect.TypeOf(graphs[0].graph.usedEdges), reflect.TypeOf(graphs[0].graph.laterOwners),
		reflect.TypeOf(graphs[0].graph.objects),
	} {
		if size := keyed.Key().Size(); size != 8 {
			test.Errorf("the map %v's key is %d bytes, not one word", keyed, size)
		}
	}
	filled := 0
	allocations := testing.AllocsPerRun(runs, func() {
		graphs[filled].fill()
		filled++
	})
	if allocations != expected {
		test.Errorf("filling a graph of %d methods allocated %.0f times, not %d: a node kept lists of its own, the "+
			"node list, a list of links or a map grew, or a node was worded as it was made", storageMethods,
			allocations, expected)
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

// TestOwnershipIsLinkedOncePerPair holds addOwned to one link for each owner and owned node, however often and in
// whatever order the walk sees the pair: the first owner is answered by firstOwners, every later one by laterOwners,
// and a pair either holds is never linked again. The order asks each answer twice over, the first owner straight
// after itself and again after a later one, and a later owner before and after the first.
func TestOwnershipIsLinkedOncePerPair(test *testing.T) {
	test.Parallel()
	storage := newStorageGraph()
	owned := storage.methods[0]
	owners := storage.methods[1:4]
	for _, owner := range []int{0, 0, 1, 0, 1, 2, 2, 0, 1} {
		storage.graph.see(owned, owners[owner])
	}
	ownedNode := storage.graph.node(owned)
	for index, owner := range owners {
		links := 0
		ownerNode := storage.graph.node(owner)
		for link := storage.graph.nodes[ownerNode].firstOwned; link != 0; link = storage.graph.owns[link].next {
			if storage.graph.owns[link].to == ownedNode {
				links++
			}
		}
		if links != 1 {
			test.Errorf("owner %d owns the node in %d links, not one", index, links)
		}
	}
	if len(storage.graph.owns) != 1+len(owners) {
		test.Errorf("the graph holds %d ownerships, not the %d pairs it was given", len(storage.graph.owns)-1,
			len(owners))
	}
	if len(storage.graph.firstOwners) != len(storage.graph.nodes) {
		test.Errorf("the graph holds %d first owners for its %d nodes", len(storage.graph.firstOwners),
			len(storage.graph.nodes))
	}
}
