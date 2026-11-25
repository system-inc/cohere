package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
)

// openedExecutable is the running executable as the process opened it while starting, a file held open for the
// process's life, or why it could not be opened.
type openedExecutable struct {
	path      string
	file      *os.File
	openError error
}

// openExecutable opens the running executable by the path the system gives it.
func openExecutable() openedExecutable {
	executable, executableError := os.Executable()
	if executableError != nil {
		return openedExecutable{openError: fmt.Errorf("finding the running executable: %w", executableError)}
	}
	return openedExecutableAt(executable)
}

// openedExecutableAt is the file at path opened as the executable is, held open for the process's life: nothing
// closes it, since the identity may be asked at any time, and one descriptor is all it costs.
func openedExecutableAt(path string) openedExecutable {
	file, openError := os.Open(path)
	if openError != nil {
		return openedExecutable{path: path, openError: fmt.Errorf("opening the running executable %s: %w", path,
			openError)}
	}
	return openedExecutable{path: path, file: file}
}

// digest is the sha256 of the opened file's bytes, read through its descriptor from the start, whatever the path
// names now. A section reader reads at offsets, so no other reader's position moves it.
func (opened openedExecutable) digest() (string, error) {
	if opened.openError != nil {
		return "", opened.openError
	}
	digester := sha256.New()
	if _, copyError := io.Copy(digester, io.NewSectionReader(opened.file, 0, math.MaxInt64)); copyError != nil {
		return "", fmt.Errorf("hashing the running executable %s: %w", opened.path, copyError)
	}
	return hex.EncodeToString(digester.Sum(nil)), nil
}
