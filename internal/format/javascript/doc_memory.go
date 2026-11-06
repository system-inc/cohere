package javascript

import (
	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/doc"
)

// docMemory is where one format's docs come from: concat's parts, groups and indents, cut from chunks that
// are reset once the doc is laid out and reused by the next format (#fyw36kf). Made one at a time they
// were concat's 3.9M slices, and 1.7M groups and indents, a pass on ahra. A released doc reads as
// poisonedDoc under cohere_poison, so a doc read after its file was printed shows in the output.
//
// An array doc from here is a *doc.Sequence: a Concat is a slice, and putting one in a Doc allocates a cell
// for its header every time, 3.6M of them a pass on ahra; a pointer from the arena costs nothing.
type docMemory struct {
	parts     arena.Slab[doc.Doc]
	sequences arena.Arena[doc.Sequence]
	groups    arena.Arena[doc.Group]
	indents   arena.Arena[doc.Indent]
}

// release resets the memory and returns it to the pool. Every doc it handed out is gone.
func (memory *docMemory) release() {
	memory.parts.Reset()
	memory.sequences.Reset()
	memory.groups.Reset()
	memory.indents.Reset()
	docMemories.Put(memory)
}
