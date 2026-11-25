package program

import (
	"sync/atomic"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
)

// timingHost times the compiler host's GetSourceFile, which is how the program loads every file: a
// read and a parse. Everything else passes through.
type timingHost struct {
	compiler.CompilerHost
	loads  atomic.Int64
	summed atomic.Int64
}

func (host *timingHost) GetSourceFile(options ast.SourceFileParseOptions) *ast.SourceFile {
	started := time.Now()
	sourceFile := host.CompilerHost.GetSourceFile(options)
	host.loads.Add(1)
	host.summed.Add(int64(time.Since(started)))
	return sourceFile
}
