package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
)

// executableIdentity hashes the executable once per process: it is tens of megabytes, and it cannot change under
// a running process in any way that matters, since the code running is what was loaded.
var executableIdentity = sync.OnceValues(func() (string, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return "", fmt.Errorf("finding the running executable: %w", executableError)
	}
	opened, openError := os.Open(executable)
	if openError != nil {
		return "", fmt.Errorf("hashing the running executable %s: %w", executable, openError)
	}
	defer func() {
		// Ignored: the file was only read, and every byte of it hashed or the copy failed.
		_ = opened.Close()
	}()
	digester := sha256.New()
	if _, copyError := io.Copy(digester, opened); copyError != nil {
		return "", fmt.Errorf("hashing the running executable %s: %w", executable, copyError)
	}
	return hex.EncodeToString(digester.Sum(nil)), nil
})

// ExecutableIdentity is the sha256 of the running executable, memoized for the process. It stands for every rule's
// code and embedded policy at once, so an entry another build of the engine wrote is never read.
func ExecutableIdentity() (string, error) {
	return executableIdentity()
}
