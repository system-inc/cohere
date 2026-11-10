package gitignore

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Why a matcher read no tracked paths from the repository's index, each wrapped in an error naming the file. A
// matcher that fell back decides by the ignore files alone, as it did before it read the index, and says why in
// IndexFallback.
var (
	// ErrNoIndex is a root with no `.git`, a repository with no index yet (a fresh `git init`), or an index that
	// could not be read.
	ErrNoIndex = errors.New("no index says what is tracked")

	// ErrSplitIndex is an index whose `link` extension keeps most entries in a shared index: what is tracked is the
	// two merged by two bitmaps, which this reader does not do rather than guess at.
	ErrSplitIndex = errors.New(
		"is a split index, whose entries live partly in a shared index this reader does not merge",
	)

	// ErrSparseIndex is an index holding sparse directory entries, each standing for every path below a directory
	// without naming one.
	ErrSparseIndex = errors.New("is a sparse index, whose directory entries name no path below them")

	// ErrMalformedIndex is a file that is not an index git writes: a wrong signature or version, an entry or an
	// extension running past the end, a checksum that does not match, or a required extension git itself would
	// refuse.
	ErrMalformedIndex = errors.New("is not an index git writes")
)

// indexSignature opens every index: "dircache".
var indexSignature = []byte("DIRC")

// indexEntryStatBytes is the stat data and mode opening every entry: ten 32-bit fields.
const indexEntryStatBytes = 40

// The entry flags' bits: the extended flag, which says a second 16-bit word of flags follows, and the name's
// length, which saturates at its mask for a longer name.
const (
	indexFlagExtended   = 0x4000
	indexFlagNameLength = 0x0FFF
)

// indexModeDirectory is a sparse directory entry's object type, a tree's, in the mode's top four bits.
const indexModeDirectory = 0o040000

// trackedIndex is every path a repository's index tracks, sorted as the index sorts them, by bytes, each once, and
// every entry it holds with its mode, which TrackedIgnoredDigest reads.
type trackedIndex struct {
	paths []string

	// entries is every entry of the index, each stage of a conflict its own, sorted by path and then mode, so two
	// indexes that track the same paths at the same modes hold the same entries whatever order git wrote them in.
	entries []indexEntry
}

// indexEntry is one entry of an index: its path and its mode, which says whether it is a file, an executable, a
// symbolic link or a gitlink, a submodule's commit.
type indexEntry struct {
	path string
	mode uint32
}

// tracks reports whether relativePath is tracked, or is a directory holding a tracked path, which is what git's
// check-ignore asks of the index before any pattern: a pathspec naming a directory matches every entry below it.
func (index *trackedIndex) tracks(relativePath string) bool {
	if index == nil || relativePath == "" {
		return false
	}
	position, found := slices.BinarySearch(index.paths, relativePath)
	if found {
		return true
	}
	// The index sorts by bytes, so every path below a directory sits together, from the first at or after
	// "directory/". A sibling such as "directory.go" sorts before it and "directory0" after.
	directoryPrefix := relativePath + "/"
	below, _ := slices.BinarySearch(index.paths[position:], directoryPrefix)
	below += position
	return below < len(index.paths) && strings.HasPrefix(index.paths[below], directoryPrefix)
}

// IndexFile is the index git reads for the repository whose `.git` is marker: `.git/index` when it is a directory,
// and the index in the git directory a `.git` file points to, a worktree's or a submodule's, when it is a file. A
// pointer that cannot be read gives `.git/index` below the file, which is never there, as git cannot read such a
// repository either.
func IndexFile(marker string, isDirectory bool) string {
	gitDirectory, gitDirectoryError := gitDirectoryOf(marker, isDirectory)
	if gitDirectoryError != nil {
		return filepath.Join(marker, "index")
	}
	return filepath.Join(gitDirectory, "index")
}

// gitDirectoryOf is the git directory of the repository whose `.git` is marker: marker itself when it is a
// directory, and the directory its `gitdir:` line names, relative to marker's own directory or absolute, when it is
// a file.
func gitDirectoryOf(marker string, isDirectory bool) (string, error) {
	if isDirectory {
		return marker, nil
	}
	contents, readError := os.ReadFile(marker)
	if readError != nil {
		return "", fmt.Errorf("gitignore: reading %s: %w", marker, readError)
	}
	gitDirectory, found := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
	if !found {
		return "", fmt.Errorf("gitignore: %s is a file that names no git directory with a `gitdir:` line", marker)
	}
	gitDirectory = strings.TrimSpace(gitDirectory)
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(filepath.Dir(marker), gitDirectory)
	}
	return filepath.Clean(gitDirectory), nil
}

// readRepositoryIndex reads the index of the repository whose `.git` is marker, and the file it read or looked for.
// Every error is a reason to fall back to the ignore files alone, and wraps one of the Err values above.
func readRepositoryIndex(marker string, isDirectory bool) (*trackedIndex, string, error) {
	gitDirectory, gitDirectoryError := gitDirectoryOf(marker, isDirectory)
	if gitDirectoryError != nil {
		return nil, filepath.Join(marker, "index"), fmt.Errorf("%w: %w", ErrNoIndex, gitDirectoryError)
	}
	indexPath := filepath.Join(gitDirectory, "index")
	hashSize, objectFormatError := objectFormatOf(gitDirectory)
	if objectFormatError != nil {
		return nil, indexPath, objectFormatError
	}
	contents, readError := os.ReadFile(indexPath)
	if errors.Is(readError, fs.ErrNotExist) {
		return nil, indexPath, fmt.Errorf("%w: %s does not exist, so nothing is tracked yet", ErrNoIndex, indexPath)
	}
	if readError != nil {
		return nil, indexPath, fmt.Errorf("%w: reading %s: %w", ErrNoIndex, indexPath, readError)
	}
	index, parseError := parseIndex(contents, hashSize)
	if parseError != nil {
		return nil, indexPath, fmt.Errorf("gitignore: %s %w", indexPath, parseError)
	}
	return index, indexPath, nil
}

// objectFormatOf is the size of the repository's object names, in bytes, which every index entry and the index's
// checksum are sized by: SHA-1's unless the repository's config sets extensions.objectFormat. The config is the
// common directory's, which a worktree's git directory names in its `commondir` file.
func objectFormatOf(gitDirectory string) (int, error) {
	configurationPath := filepath.Join(commonDirectoryOf(gitDirectory), "config")
	configuration, readError := os.ReadFile(configurationPath)
	if errors.Is(readError, fs.ErrNotExist) {
		return sha1.Size, nil
	}
	if readError != nil {
		return 0, fmt.Errorf("%w: reading %s for the repository's object format: %w", ErrNoIndex, configurationPath,
			readError)
	}
	switch format := configurationObjectFormat(configuration); format {
	case "", "sha1":
		return sha1.Size, nil
	case "sha256":
		return sha256.Size, nil
	default:
		return 0, fmt.Errorf("%w: %s names the object format %q, which git writes no index in", ErrNoIndex,
			configurationPath, format)
	}
}

// commonDirectoryOf is the directory a git directory shares with its repository's other worktrees, where the config
// and info/exclude live: the one its `commondir` file names, relative to it or absolute, and the git directory itself
// when it has none, as a clone's and a submodule's have none.
func commonDirectoryOf(gitDirectory string) string {
	pointer, readError := os.ReadFile(filepath.Join(gitDirectory, "commondir"))
	if readError != nil {
		return gitDirectory
	}
	commonDirectory := strings.TrimSpace(string(pointer))
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(gitDirectory, commonDirectory)
	}
	return filepath.Clean(commonDirectory)
}

// configurationObjectFormat is the value of extensions.objectFormat in a git config file, "" when it sets none.
// Section and key names are case insensitive, as git reads them; a value may be quoted, and a `#` or `;` outside
// quotes starts a comment.
func configurationObjectFormat(configuration []byte) string {
	inExtensions := false
	format := ""
	scanner := bufio.NewScanner(bytes.NewReader(configuration))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			closing := strings.IndexByte(line, ']')
			if closing < 0 {
				inExtensions = false
				continue
			}
			inExtensions = strings.EqualFold(strings.TrimSpace(line[1:closing]), "extensions")
			// git reads a key on the same line as its section header.
			line = strings.TrimSpace(line[closing+1:])
		}
		if !inExtensions {
			continue
		}
		key, value, assigned := strings.Cut(line, "=")
		if !assigned || !strings.EqualFold(strings.TrimSpace(key), "objectformat") {
			continue
		}
		// The last assignment wins, as in git.
		format = configurationValue(value)
	}
	return format
}

// configurationValue is a config value with its comment, surrounding space and quotes removed.
func configurationValue(raw string) string {
	var value strings.Builder
	quoted := false
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		switch {
		case character == '"':
			quoted = !quoted
		case !quoted && (character == '#' || character == ';'):
			return strings.TrimSpace(value.String())
		default:
			value.WriteByte(character)
		}
	}
	return strings.TrimSpace(value.String())
}

// parseIndex reads an index of version 2, 3 or 4 whose object names are hashSize bytes: every path it tracks, each
// once, sorted by bytes, and every entry with its mode. Every entry is a tracked path whatever its flags say: each
// stage of a conflict, a skip-worktree entry and an intent-to-add entry alike, as git's check-ignore reads them.
func parseIndex(contents []byte, hashSize int) (*trackedIndex, error) {
	if len(contents) < 12+hashSize || !bytes.Equal(contents[:4], indexSignature) {
		return nil, fmt.Errorf("%w: it does not open with git's DIRC signature", ErrMalformedIndex)
	}
	version := binary.BigEndian.Uint32(contents[4:8])
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("%w: its version is %d, and git writes 2, 3 and 4", ErrMalformedIndex, version)
	}
	body := contents[:len(contents)-hashSize]
	if checksumError := verifyIndexChecksum(body, contents[len(body):], hashSize); checksumError != nil {
		return nil, checksumError
	}
	entryCount := binary.BigEndian.Uint32(contents[8:12])

	// The paths of version 2 and 3 are read in place, as substrings of one copy of the file, so a large index costs
	// one allocation for its names rather than one per entry. Version 4's are rebuilt from the previous path.
	text := string(body)
	paths := make([]string, 0, entryCount)
	entries := make([]indexEntry, 0, entryCount)
	offset := 12
	previous := ""
	for entry := uint32(0); entry < entryCount; entry++ {
		flagsOffset := offset + indexEntryStatBytes + hashSize
		if flagsOffset+2 > len(body) {
			return nil, fmt.Errorf("%w: entry %d runs past the end of its entries", ErrMalformedIndex, entry+1)
		}
		mode := binary.BigEndian.Uint32(body[offset+24 : offset+28])
		if mode&0o170000 == indexModeDirectory {
			return nil, ErrSparseIndex
		}
		flags := binary.BigEndian.Uint16(body[flagsOffset : flagsOffset+2])
		nameOffset := flagsOffset + 2
		if flags&indexFlagExtended != 0 {
			if version < 3 {
				return nil, fmt.Errorf("%w: entry %d sets the extended flag, which version 2 has no room for",
					ErrMalformedIndex, entry+1)
			}
			nameOffset += 2
		}
		var name string
		if version == 4 {
			removed, varintLength := decodeIndexVarint(body[min(nameOffset, len(body)):])
			if varintLength == 0 || removed > uint64(len(previous)) {
				return nil, fmt.Errorf("%w: entry %d removes more of the previous path than it holds",
					ErrMalformedIndex, entry+1)
			}
			suffixOffset := nameOffset + varintLength
			terminator := bytes.IndexByte(body[suffixOffset:], 0)
			if terminator < 0 {
				return nil, fmt.Errorf("%w: entry %d's path has no end", ErrMalformedIndex, entry+1)
			}
			name = previous[:len(previous)-int(removed)] + text[suffixOffset:suffixOffset+terminator]
			offset = suffixOffset + terminator + 1
		} else {
			if nameOffset > len(body) {
				return nil, fmt.Errorf("%w: entry %d runs past the end of its entries", ErrMalformedIndex, entry+1)
			}
			terminator := bytes.IndexByte(body[nameOffset:], 0)
			if terminator < 0 {
				return nil, fmt.Errorf("%w: entry %d's path has no end", ErrMalformedIndex, entry+1)
			}
			name = text[nameOffset : nameOffset+terminator]
			// Padded with one to eight NUL bytes to a multiple of eight from the entry's start.
			offset += (nameOffset - offset + terminator + 8) &^ 7
			if offset > len(body) {
				return nil, fmt.Errorf("%w: entry %d's padding runs past the end of its entries",
					ErrMalformedIndex, entry+1)
			}
		}
		if length := int(flags & indexFlagNameLength); length != indexFlagNameLength && length != len(name) {
			return nil, fmt.Errorf("%w: entry %d's path, %q, is not the %d bytes its flags say", ErrMalformedIndex,
				entry+1, name, length)
		}
		// A sparse directory entry is also a path ending in `/`, whatever an older writer put in its mode.
		if strings.HasSuffix(name, "/") {
			return nil, ErrSparseIndex
		}
		entries = append(entries, indexEntry{path: name, mode: mode})
		// The stages of one conflicted path sit together; the path is tracked once.
		if len(paths) == 0 || paths[len(paths)-1] != name {
			paths = append(paths, name)
		}
		previous = name
	}
	if extensionError := readIndexExtensions(body[offset:]); extensionError != nil {
		return nil, extensionError
	}
	// git writes its entries sorted, and the lookups binary search them, so an index out of order is sorted rather
	// than misread.
	if !slices.IsSorted(paths) {
		slices.Sort(paths)
		paths = slices.Compact(paths)
	}
	slices.SortStableFunc(entries, func(left indexEntry, right indexEntry) int {
		if order := strings.Compare(left.path, right.path); order != 0 {
			return order
		}
		return cmp.Compare(left.mode, right.mode)
	})
	return &trackedIndex{paths: paths, entries: entries}, nil
}

// verifyIndexChecksum holds the index to its trailing checksum, which git writes over everything before it. One that
// is all zeros is git's index.skipHash, which writes none.
func verifyIndexChecksum(body []byte, checksum []byte, hashSize int) error {
	if !slices.ContainsFunc(checksum, func(value byte) bool { return value != 0 }) {
		return nil
	}
	var digest hash.Hash
	if hashSize == sha256.Size {
		digest = sha256.New()
	} else {
		digest = sha1.New()
	}
	digest.Write(body)
	if !bytes.Equal(digest.Sum(nil), checksum) {
		return fmt.Errorf("%w: its checksum does not match its contents, as a write cut short leaves it",
			ErrMalformedIndex)
	}
	return nil
}

// readIndexExtensions walks the extensions after the entries: a four-byte signature, a 32-bit size, and that many
// bytes. One whose signature opens with a capital is optional and skipped, as git skips it. Any other is required:
// `link` is a split index and `sdir` a sparse one, and one git does not know it refuses, so it is refused here too.
func readIndexExtensions(extensions []byte) error {
	remaining := extensions
	for len(remaining) > 0 {
		if len(remaining) < 8 {
			return fmt.Errorf("%w: %d bytes after its entries are too few for an extension", ErrMalformedIndex,
				len(remaining))
		}
		signature := string(remaining[:4])
		size := binary.BigEndian.Uint32(remaining[4:8])
		if uint64(size) > uint64(len(remaining)-8) {
			return fmt.Errorf("%w: its %q extension runs past the end", ErrMalformedIndex, signature)
		}
		switch {
		case signature == "link":
			return ErrSplitIndex
		case signature == "sdir":
			return ErrSparseIndex
		case signature[0] < 'A' || signature[0] > 'Z':
			return fmt.Errorf("%w: it holds the required extension %q, which git itself does not understand",
				ErrMalformedIndex, signature)
		}
		remaining = remaining[8+size:]
	}
	return nil
}

// decodeIndexVarint reads version 4's prefix length, in the encoding of a pack's offset deltas: seven bits a byte,
// most significant first, each continuation adding one before the shift. It returns the value and the bytes read,
// zero for an encoding that runs out or overflows.
func decodeIndexVarint(encoded []byte) (uint64, int) {
	if len(encoded) == 0 {
		return 0, 0
	}
	character := encoded[0]
	value := uint64(character & 0x7f)
	read := 1
	for character&0x80 != 0 {
		if read >= len(encoded) || value+1 > (^uint64(0))>>7 {
			return 0, 0
		}
		value++
		character = encoded[read]
		read++
		value = (value << 7) | uint64(character&0x7f)
	}
	return value, read
}
