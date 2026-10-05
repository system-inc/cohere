package estree

import (
	"sync"

	"github.com/system-inc/cohere/internal/format/arena"
)

// Arena is where one format's tree comes from: its nodes, and the properties each node holds, cut from
// chunks that are reset when the file is printed and reused for the next (#fyw36kf). New made each node and
// its properties on the heap, two allocations per node, 9.1M of formatting's allocations on ahra.
//
// A nil *Arena allocates on the heap, as New does. Under the cohere_poison build tag a released node reads
// as poisonedNodeType, so a node read after its file was printed changes the output where the corpus tests
// see it.
type Arena struct {
	nodes      arena.Arena[Node]
	properties arena.Slab[property]
}

// poisonedNodeType is what a released node's type reads under cohere_poison: no parse produces it.
const poisonedNodeType = "CoherePoisonedNode"

var arenas = sync.Pool{New: func() any {
	nodes := &Arena{}
	nodes.nodes.Poison = Node{nodeType: poisonedNodeType}
	nodes.properties.Poison = property{key: poisonedNodeType}
	return nodes
}}

// AcquireArena is an arena for one format, from the pool. Release it once the file is printed and nothing
// will read its tree again.
func AcquireArena() *Arena { return arenas.Get().(*Arena) }

// Release resets the arena and returns it to the pool. Every node it handed out is gone.
func (nodes *Arena) Release() {
	if nodes == nil {
		return
	}
	nodes.nodes.Reset()
	nodes.properties.Reset()
	arenas.Put(nodes)
}

// node is New, from the arena.
func (nodes *Arena) node(nodeType string, start int, end int, keysAndValues []any) *Node {
	if nodes == nil {
		return New(nodeType, start, end, keysAndValues...)
	}
	created := nodes.nodes.New(Node{nodeType: nodeType, Range: [2]int{start, end}, properties: nodes.properties.Make(len(keysAndValues) / 2)})
	for index := 0; index+1 < len(keysAndValues); index += 2 {
		created.Set(keysAndValues[index].(string), keysAndValues[index+1])
	}
	return created
}
