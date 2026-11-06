package next

import (
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// countingProgram is a Program whose file system counts the existence checks asked of it, by path.
type countingProgram struct {
	rule.Program
	fileSystem *countingFileSystem
}

func (program countingProgram) FileSystem() vfs.FS { return program.fileSystem }
