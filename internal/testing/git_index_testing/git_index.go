// Package git_index_testing is for tests that need a repository whose index tracks given paths, written without a git
// child: each git child forks the test process, and on darwin a fork makes the whole heap fault again on its next write
// (#2htryg8), and a git child reads the machine's own git configuration. The fixtures in
// internal/gitignore/testdata/index are git's own bytes; this writes the small indexes a test needs around a tree.
package git_index_testing

import (
	"crypto/sha1"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
)

// entryStatBytes is the stat data and mode opening every index entry: ten 32-bit fields.
const entryStatBytes = 40

// nameLengthMask is the entry flags' bits holding the name's length, which saturates at the mask for a longer name.
const nameLengthMask = 0x0FFF

// Write writes a version 2 index of paths, each slash separated and relative to the repository's root, a regular file
// at stage 0, as gitDirectory's index, with SHA-1 object names and the checksum git writes, making gitDirectory when it
// is not there.
func Write(gitDirectory string, paths ...string) error {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	contents := []byte("DIRC")
	contents = binary.BigEndian.AppendUint32(contents, 2)
	contents = binary.BigEndian.AppendUint32(contents, uint32(len(sorted)))
	for _, path := range sorted {
		entry := make([]byte, entryStatBytes+sha1.Size)
		binary.BigEndian.PutUint32(entry[24:28], 0o100644)
		entry = binary.BigEndian.AppendUint16(entry, uint16(min(len(path), nameLengthMask)))
		entry = append(entry, path...)
		// One to eight NUL bytes, to a multiple of eight.
		entry = append(entry, make([]byte, 8-len(entry)%8)...)
		contents = append(contents, entry...)
	}
	checksum := sha1.Sum(contents)
	contents = append(contents, checksum[:]...)
	if makeError := os.MkdirAll(gitDirectory, 0o755); makeError != nil {
		return makeError
	}
	return os.WriteFile(filepath.Join(gitDirectory, "index"), contents, 0o644)
}
