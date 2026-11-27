package written_repository

import (
	"cmp"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/git_tree"
)

// indexEntryStatBytes is the stat data and mode opening every entry: ten 32-bit fields. A written entry's stat data
// is zero, which git reads as a file it must hash again to call clean, as it does after `git update-index
// --cacheinfo`.
const indexEntryStatBytes = 40

// indexEntryModeOffset is where the mode sits among the stat fields, the seventh.
const indexEntryModeOffset = 24

// indexFlagNameLength is the bits of an entry's flags that hold its path's length, saturated for a longer path.
const indexFlagNameLength = 0x0FFF

// indexFlagStageShift places an entry's stage in its flags, above the path's length.
const indexFlagStageShift = 12

// writeIndex writes gitDirectory's index of tracked as git writes one, version 2 with SHA-1 object names and no
// extension, sorted by path and then stage as git sorts it, so `git ls-files --stage` and cohere's own index reader
// (internal/gitignore) read it back.
func writeIndex(gitDirectory string, worktree string, tracked []git_tree.IndexEntry) error {
	entries := slices.Clone(tracked)
	for position, entry := range entries {
		if entry.Object != "" {
			continue
		}
		path := filepath.Join(worktree, filepath.FromSlash(entry.Path))
		mode, object, hashError := git_tree.HashFile(git_tree.Sha1, path)
		if hashError != nil {
			return fmt.Errorf("recording %s: %w", entry.Path, hashError)
		}
		entries[position].Mode, entries[position].Object = mode, object
	}
	slices.SortFunc(entries, func(left git_tree.IndexEntry, right git_tree.IndexEntry) int {
		return cmp.Or(strings.Compare(left.Path, right.Path), cmp.Compare(left.Stage, right.Stage))
	})
	contents := []byte("DIRC")
	contents = binary.BigEndian.AppendUint32(contents, 2)
	contents = binary.BigEndian.AppendUint32(contents, uint32(len(entries)))
	for _, entry := range entries {
		object, decodeError := hex.DecodeString(entry.Object)
		if decodeError != nil || len(object) != sha1.Size {
			return fmt.Errorf("%s's object %q is no SHA-1 name", entry.Path, entry.Object)
		}
		if entry.Stage < 0 || entry.Stage > 3 {
			return fmt.Errorf("%s's stage %d is none git records", entry.Path, entry.Stage)
		}
		record := make([]byte, indexEntryStatBytes)
		binary.BigEndian.PutUint32(record[indexEntryModeOffset:], uint32(entry.Mode))
		record = append(record, object...)
		record = binary.BigEndian.AppendUint16(record,
			uint16(entry.Stage<<indexFlagStageShift|min(len(entry.Path), indexFlagNameLength)))
		record = append(record, entry.Path...)
		// One to eight zero bytes, ending the path and padding the entry to a multiple of eight.
		record = append(record, make([]byte, 8-len(record)%8)...)
		contents = append(contents, record...)
	}
	checksum := sha1.Sum(contents)
	contents = append(contents, checksum[:]...)
	return os.WriteFile(filepath.Join(gitDirectory, "index"), contents, 0o644)
}
