// Package executable_testing is for tests that write an executable, a stand-in on PATH, an engine a command under
// test starts or a copy of a binary, and then run it. Written with os.WriteFile, such a run can fail on Linux with
// `text file busy` while a parallel test in the same process starts a process of its own (Go issue 22315).
package executable_testing

import (
	"io/fs"
	"os"
	"syscall"
)

// WriteFile writes contents to path as os.WriteFile does, creating the file with mode or truncating it, with no fork
// in this process while the file is open for writing, so the file can be run as soon as it returns.
//
// Linux refuses to exec a file any process holds open for writing (`ETXTBSY`). A fork elsewhere in this process
// between the open and the close hands the child a copy of the descriptor, which it keeps until its own exec closes it
// under `O_CLOEXEC`, so a run of the file in that window fails. macOS enforces no such rule, which is why only Linux
// sees it. Writing elsewhere and renaming into place is no cure: the child's copy is of the same inode, which a rename
// keeps. Every fork holds syscall.ForkLock for writing, so holding it for reading across the open, the write and the
// close keeps any fork from starting until the descriptor is gone. Reading, not writing: the lock's documentation
// gives reading to code that makes a descriptor, and on Linux a writer here would wait as long as forks overlap,
// while the runtime lets a waiting reader in.
func WriteFile(path string, contents []byte, mode fs.FileMode) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	file, openError := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if openError != nil {
		return openError
	}
	if _, writeError := file.Write(contents); writeError != nil {
		// Ignored: the write's failure is the one that says what went wrong, and the close only lets the descriptor go.
		_ = file.Close()
		return writeError
	}
	return file.Close()
}
