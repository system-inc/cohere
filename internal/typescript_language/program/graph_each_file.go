package program

import (
	"context"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

// EachFileOnItsChecker calls visit for every file with the checker that owns it, as many files at once as there are
// checkers. Each worker takes the files of the checkers the walk would give it (assignFilesToWorkers), so no two
// goroutines ask one checker at once, and a file is never visited on a checker that does not own it, whose answers
// could not be mixed with its owner's. visit runs on a worker's goroutine, with the file's index in files, and must
// guard whatever it shares.
//
// For a reader that needs every file's checker after the walk, as the duplicates report lowers every function
// (#jfvhbj7), rather than asking file by file on one goroutine, which serializes on one core what the checkers can
// do side by side.
func (graph *Graph) EachFileOnItsChecker(
	callerContext context.Context,
	files []*ast.SourceFile,
	visit func(index int, sourceFile *ast.SourceFile, typeChecker *checker.Checker),
) {
	if len(files) == 0 {
		return
	}
	assignments := graph.assignFilesToWorkers(callerContext, files, graph.Workers(), true)
	var group sync.WaitGroup
	for _, indices := range assignments {
		group.Go(func() {
			for _, index := range indices {
				typeChecker, release := graph.CheckerForFile(callerContext, files[index])
				visit(index, files[index], typeChecker)
				release()
			}
		})
	}
	group.Wait()
}
