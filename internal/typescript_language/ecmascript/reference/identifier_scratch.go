package reference

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// identifierScratch is buildIdentifierIndex's walk buffers: every identifier in walk order, and each one's
// name's place in spans.
type identifierScratch struct {
	walked    []*ast.Node
	positions []int32
}

// release clears the node pointers, so a pooled buffer never keeps a file's tree alive, and returns the
// buffers to the pool.
func (scratch *identifierScratch) release() {
	clear(scratch.walked)
	identifierScratchPool.Put(scratch)
}
