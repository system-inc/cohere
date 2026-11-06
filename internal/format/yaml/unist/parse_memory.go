package unist

import (
	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/yaml/compose"
	"github.com/system-inc/cohere/internal/format/yaml/cst"
)

// parseMemory is what one Parse uses only while it runs, kept for the next (#vbjv3d6): its point and
// position objects, which Parse copies into each node's Position before it returns, and its tables: the
// text as UTF-16 units, each unit's byte offset, and the nodes' positions (#v6ksqg3).
type parseMemory struct {
	points    arena.Arena[point]
	positions arena.Arena[position]

	units           []uint16
	offsets         unitOffsets
	positionsByNode map[*Node]*position

	// tokens and composeNodes are the CST's tokens and the composer's scalars, pairs and collections.
	// Both die with the parse: a unist node keeps neither, only strings and positions copied out of them
	// (#v6ksqg3).
	tokens       arena.Arena[cst.Token]
	composeNodes arena.Arena[compose.Node]
}

func (memory *parseMemory) reset() {
	memory.points.Reset()
	memory.positions.Reset()
	memory.tokens.Reset()
	memory.composeNodes.Reset()
	// Under cohere_poison a released table reads a noncharacter and an offset past any text, so a parse that
	// read one after its release prints wrong or stops. A cleared map answers nil, which stops it too.
	arena.Release(memory.units, 0xFFFF)
	arena.Release(memory.offsets, 1<<40)
	clear(memory.positionsByNode)
}
