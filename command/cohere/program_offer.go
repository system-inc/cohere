package main

import (
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/types/program"
)

// programOffer is where a speculation finds the program once it is built. Its zero value has none, and so does
// a nil one.
type programOffer struct {
	program atomic.Pointer[compiler.Program]
	// graph is the graph the program was built in, set before the program, so a speculation that sees the
	// program can ask the graph whether its copy of a file is still the file's.
	graph atomic.Pointer[program.Graph]
}

// offer hands the speculation the graph's program, once it is built.
func (programs *programOffer) offer(graph *program.Graph) {
	if programs != nil && graph != nil && graph.Program != nil {
		programs.graph.Store(graph)
		programs.program.Store(graph.Program)
	}
}

// unchangedTreeOf is boundTreeOf's tree when the program's copy of the file is the file's bytes, by the stat it
// was read under and a file that starts with no byte order mark (Graph.ReadUnchanged): the speculation then formats the
// tree's own text and reads nothing (#q6dey77). Rereading every file to compare it with the tree was 51 MB on a cold
// ahra run. A file changed on disk since the program read it has another stat, so it is read as before, its bytes
// differ from the program's, and keepable discards the attempt: an edit made after the walk read the file is still
// noticed.
func (programs *programOffer) unchangedTreeOf(fileName string) *ast.SourceFile {
	tree := programs.boundTreeOf(fileName)
	if tree == nil || !programs.graph.Load().ReadUnchanged(fileName, tree.Text()) {
		return nil
	}
	return tree
}

// boundTreeOf is the program's tree of fileName if the program is in and has bound that file, or nil.
//
// Bound, because the binder writes node flags the formatter's converter reads (binder.go sets
// NodeFlagsThisNodeOrAnySubNodesHasError and NodeFlagsExportContext on nodes, and estree reads Let, Const,
// Using and Reparsed off the same word), and the checkers bind on their own goroutines while this pass runs.
// isBound is an atomic stored once the binder is done with the file, so a file that reads bound here has had
// every flag written before any is read. A file not yet bound is not waited for: it is parsed, as before.
//
// The tree is not yet known to be of the bytes the speculation reads; edit.CheckFileSeeded compares them.
func (programs *programOffer) boundTreeOf(fileName string) *ast.SourceFile {
	if programs == nil {
		return nil
	}
	program := programs.program.Load()
	if program == nil {
		return nil
	}
	sourceFile := program.GetSourceFile(tspath.RootedFilePath(fileName))
	if sourceFile == nil || !sourceFile.IsBound() {
		return nil
	}
	return sourceFile
}
