package program

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The resolution cache: what module resolution found last run, replayed instead of re-walked.
//
// Built and deliberately not enabled. Nothing wires this into the graph build, and that is a
// decision rather than unfinished work.
//
// The reason is a number. The estimate this was built on was 115 to 220ms, derived by taking
// a cold-versus-warm difference, subtracting the measured cost of reading source bytes, and
// attributing the remainder to module-resolution lookups. The remainder was real. Attributing
// all of it to something a cache could remove was not.
//
// Measured properly, by wrapping vfs.FS and counting actual calls, then recording those
// lookups and replaying them with the resulting program asserted identical:
//
//	against a raw filesystem       208ms -> 133ms, saved 75ms
//	against cachedvfs, what ships  217ms -> 175ms, saved 42ms
//
// Both with zero lookup misses and 9,982 files on each side. cachedvfs already memoizes within
// a run, so a persisted cache only replaces the first occurrence of each distinct lookup, and
// the repeats were free already. The traffic is 29,050 FileExists, 5,672 DirectoryExists and 750
// Realpath against 10,814 unavoidable ReadFile.
//
// 42ms does not pay for the risk. An invalidation bug in a signature cache costs a stale
// finding; an invalidation bug here costs a wrong program, because a resolution decides which
// files are in it at all. The AST cache was killed at 28 to 41ms for the same reason.
//
// It stays on disk because the format and its guards are tested and cost nothing sitting here,
// and because the trade reverses if lookups ever get expensive again: a network filesystem, a
// container mount, or an upstream change that drops the cachedvfs layer would each move the
// 42ms figure a lot. Re-run the measurement before enabling it, rather than trusting this note.
//
// Module resolution asks the filesystem where a specifier lives, and most of what it asks
// does not exist. Our build info records 1,959 package.json paths the compiler looked for and
// did not find, every single run. The filesystem half of the graph phase measures 274 to
// 381ms, of which roughly 159ms is reading source bytes that parsing needs regardless.
//
// The remainder is lookup traffic, and this replaces the part of it that is not already
// memoized within the run. That distinction is the whole story of the 42ms above, and it is
// why the residual is not the saving.
//
// The replay itself is nearly free: 30,781 resolutions read back and rebuilt into a usable
// map in 2.3ms from a 2.27 MB artifact.
//
// This cache invalidates differently from the signature cache, and that is the single thing
// to understand before touching this file. A resolution depends on the shape of the
// filesystem, not on the contents of any file. Adding a file changes what a specifier
// resolves to while no existing file's bytes changed at all. So content hashes cannot
// invalidate this, and a cache keyed on them would serve a stale resolution forever while
// every content hash still matched.
//
// The guard is a directory fingerprint: one stat per directory resolution touches, hashed
// together. Measured at 2.3ms across 1,699 directories, cheap against anything it guards.
// A listing hash costs 38.3ms and buys only the case where an entry is renamed without the
// directory's mtime moving, which the probe below could not produce on any filesystem we run
// on.
//
// The mtime assumption is verified rather than assumed, in resolutioncache_test.go: adding,
// removing, and renaming a file all move the directory's mtime, and editing a file's contents
// correctly does not.
type ResolutionCache struct {
	// Fingerprint is the hash of every watched directory's mtime, taken when the cache was
	// written. A different fingerprint now means the filesystem shape moved and every stored
	// resolution is suspect.
	Fingerprint [sha256.Size]byte

	// Directories are the paths the fingerprint covers, kept so a later run fingerprints the
	// same set rather than whatever it happens to derive.
	Directories []string

	// Entries maps a resolution request to what it resolved to. An empty ResolvedFileName is
	// a real answer: it means the specifier did not resolve, and replaying that saves the
	// failed lookups, which are the majority of the traffic.
	Entries []ResolutionEntry
}

// ResolutionEntry is one specifier, resolved from one file, and what it produced.
type ResolutionEntry struct {
	FromFile         string
	Specifier        string
	Mode             int32
	ResolvedFileName string
	Extension        string
	Flags            int32
}

// Resolution entry flags, packed rather than stored as separate bools so the record stays
// fixed-width and the artifact stays mmap-shaped.
const (
	ResolutionFlagExternalLibrary  int32 = 1 << 0
	ResolutionFlagUsedTsExtension  int32 = 1 << 1
	ResolutionFlagUsedExtraMatcher int32 = 1 << 2
)

// resolutionCacheMagic identifies the format and its version in the first eight bytes.
//
// A version byte is not decoration here. This artifact is read back as raw offsets into a
// blob, so a file written by an older layout does not fail to parse, it parses into garbage
// pointing at the wrong strings. Refusing to read an unrecognized version is what makes the
// failure loud instead of silently wrong.
var resolutionCacheMagic = [8]byte{'v', 'f', 'y', 'r', 'e', 's', 0, 1}

// 40 bytes: four interned strings as offset+length pairs (32), plus mode and flags (8).
// Fixed-width on purpose, so the record array can be indexed rather than walked.
const resolutionRecordSize = 40

// FingerprintDirectories hashes the mtime of every directory given, in sorted order.
//
// Sorted because a map iteration order would produce a different hash for an identical
// filesystem, which would invalidate the cache on every run while looking like the guard
// working correctly. That failure is silent in the expensive direction: nobody notices a
// cache that never hits, they just notice the tool is not faster.
//
// A directory that cannot be stat'd is folded in as a distinct marker rather than skipped.
// Skipping would make a deleted directory hash identically to one that never existed, and
// deleting a directory is exactly the kind of shape change this guards.
func FingerprintDirectories(directories []string) [sha256.Size]byte {
	sorted := make([]string, len(directories))
	copy(sorted, directories)
	sort.Strings(sorted)

	hash := sha256.New()
	stamp := make([]byte, 8)
	for _, directory := range sorted {
		hash.Write([]byte(directory))
		info, err := os.Stat(directory)
		if err != nil {
			hash.Write([]byte("\x00missing"))
			continue
		}
		binary.LittleEndian.PutUint64(stamp, uint64(info.ModTime().UnixNano()))
		hash.Write(stamp)
	}

	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

// DirectoriesOf returns the parent directory of every path given, deduplicated.
//
// These are the directories module resolution walks, and they are what the fingerprint
// covers.
func DirectoriesOf(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	directories := make([]string, 0, len(paths))
	for _, path := range paths {
		directory := filepath.Dir(path)
		if _, already := seen[directory]; already {
			continue
		}
		seen[directory] = struct{}{}
		directories = append(directories, directory)
	}
	return directories
}

// Encode writes the cache as a flat binary artifact: a header, fixed-width records, and one
// interned string blob the records point into.
//
// Interning matters more here than it looks. Every entry carries a from-file path, and the
// same file resolves many specifiers, so the paths repeat heavily; so do the resolved names,
// since many specifiers land on the same file.
func (c *ResolutionCache) Encode() []byte {
	var blob []byte
	table := make(map[string]int32)
	intern := func(value string) (int32, int32) {
		if value == "" {
			return -1, 0
		}
		if offset, seen := table[value]; seen {
			return offset, int32(len(value))
		}
		offset := int32(len(blob))
		blob = append(blob, value...)
		table[value] = offset
		return offset, int32(len(value))
	}

	type packed struct {
		fromOffset, fromLength           int32
		specifierOffset, specifierLength int32
		resolvedOffset, resolvedLength   int32
		extensionOffset, extensionLength int32
		mode, flags                      int32
	}
	records := make([]packed, 0, len(c.Entries))
	for _, entry := range c.Entries {
		fromOffset, fromLength := intern(entry.FromFile)
		specifierOffset, specifierLength := intern(entry.Specifier)
		resolvedOffset, resolvedLength := intern(entry.ResolvedFileName)
		extensionOffset, extensionLength := intern(entry.Extension)
		records = append(records, packed{
			fromOffset, fromLength,
			specifierOffset, specifierLength,
			resolvedOffset, resolvedLength,
			extensionOffset, extensionLength,
			entry.Mode, entry.Flags,
		})
	}

	// Directories ride in the blob too, as a length-prefixed run, so the reader can
	// re-fingerprint the same set without deriving it again.
	directoryOffsets := make([][2]int32, 0, len(c.Directories))
	for _, directory := range c.Directories {
		offset, length := intern(directory)
		directoryOffsets = append(directoryOffsets, [2]int32{offset, length})
	}

	headerSize := 8 + sha256.Size + 4 + 4 + 4
	recordBytes := len(records) * resolutionRecordSize
	directoryBytes := len(directoryOffsets) * 8
	buffer := make([]byte, headerSize+recordBytes+directoryBytes+len(blob))

	copy(buffer[0:], resolutionCacheMagic[:])
	copy(buffer[8:], c.Fingerprint[:])
	cursor := 8 + sha256.Size
	binary.LittleEndian.PutUint32(buffer[cursor:], uint32(len(records)))
	binary.LittleEndian.PutUint32(buffer[cursor+4:], uint32(len(directoryOffsets)))
	binary.LittleEndian.PutUint32(buffer[cursor+8:], uint32(len(blob)))
	cursor += 12

	for index, record := range records {
		offset := cursor + index*resolutionRecordSize
		binary.LittleEndian.PutUint32(buffer[offset+0:], uint32(record.fromOffset))
		binary.LittleEndian.PutUint32(buffer[offset+4:], uint32(record.fromLength))
		binary.LittleEndian.PutUint32(buffer[offset+8:], uint32(record.specifierOffset))
		binary.LittleEndian.PutUint32(buffer[offset+12:], uint32(record.specifierLength))
		binary.LittleEndian.PutUint32(buffer[offset+16:], uint32(record.resolvedOffset))
		binary.LittleEndian.PutUint32(buffer[offset+20:], uint32(record.resolvedLength))
		binary.LittleEndian.PutUint32(buffer[offset+24:], uint32(record.extensionOffset))
		binary.LittleEndian.PutUint32(buffer[offset+28:], uint32(record.extensionLength))
		binary.LittleEndian.PutUint32(buffer[offset+32:], uint32(record.mode))
		binary.LittleEndian.PutUint32(buffer[offset+36:], uint32(record.flags))
	}
	cursor += recordBytes

	for index, pair := range directoryOffsets {
		offset := cursor + index*8
		binary.LittleEndian.PutUint32(buffer[offset+0:], uint32(pair[0]))
		binary.LittleEndian.PutUint32(buffer[offset+4:], uint32(pair[1]))
	}
	cursor += directoryBytes

	copy(buffer[cursor:], blob)
	return buffer
}

// ErrResolutionCacheUnreadable means the artifact is not one this build can use: wrong magic,
// wrong version, or truncated. It is never a reason to fail a run, only a reason to resolve
// from the filesystem and write a fresh cache.
var ErrResolutionCacheUnreadable = errors.New("resolution cache is not readable by this build")

// DecodeResolutionCache reads an artifact written by Encode.
//
// Every length is checked against the buffer before it is used. A truncated or foreign file
// must produce an error rather than a slice out of range, and more importantly rather than
// entries pointing at the wrong strings: this format is raw offsets, so a corrupt artifact
// does not look corrupt, it looks like confident wrong answers.
func DecodeResolutionCache(buffer []byte) (*ResolutionCache, error) {
	headerSize := 8 + sha256.Size + 12
	if len(buffer) < headerSize {
		return nil, fmt.Errorf("%w: %d bytes is shorter than the header", ErrResolutionCacheUnreadable, len(buffer))
	}
	if string(buffer[0:8]) != string(resolutionCacheMagic[:]) {
		return nil, fmt.Errorf("%w: magic or version does not match", ErrResolutionCacheUnreadable)
	}

	cache := &ResolutionCache{}
	copy(cache.Fingerprint[:], buffer[8:8+sha256.Size])
	cursor := 8 + sha256.Size
	recordCount := int(binary.LittleEndian.Uint32(buffer[cursor:]))
	directoryCount := int(binary.LittleEndian.Uint32(buffer[cursor+4:]))
	blobLength := int(binary.LittleEndian.Uint32(buffer[cursor+8:]))
	cursor += 12

	recordBytes := recordCount * resolutionRecordSize
	directoryBytes := directoryCount * 8
	if recordCount < 0 || directoryCount < 0 || blobLength < 0 ||
		len(buffer) < cursor+recordBytes+directoryBytes+blobLength {
		return nil, fmt.Errorf("%w: header claims %d records, %d directories and %d blob bytes, "+
			"which does not fit in %d bytes", ErrResolutionCacheUnreadable,
			recordCount, directoryCount, blobLength, len(buffer))
	}

	blobStart := cursor + recordBytes + directoryBytes
	blob := buffer[blobStart : blobStart+blobLength]

	// A bounds-checked read out of the blob. An offset past the end is corruption, and
	// returning an error beats returning a string built from whatever bytes follow.
	read := func(offset, length int32) (string, error) {
		if offset < 0 {
			return "", nil
		}
		if length < 0 || int(offset)+int(length) > len(blob) {
			return "", fmt.Errorf("%w: string at %d+%d falls outside the %d-byte blob",
				ErrResolutionCacheUnreadable, offset, length, len(blob))
		}
		return string(blob[offset : offset+length]), nil
	}

	cache.Entries = make([]ResolutionEntry, 0, recordCount)
	for index := 0; index < recordCount; index++ {
		offset := cursor + index*resolutionRecordSize
		fromFile, err := read(
			int32(binary.LittleEndian.Uint32(buffer[offset+0:])),
			int32(binary.LittleEndian.Uint32(buffer[offset+4:])))
		if err != nil {
			return nil, err
		}
		specifier, err := read(
			int32(binary.LittleEndian.Uint32(buffer[offset+8:])),
			int32(binary.LittleEndian.Uint32(buffer[offset+12:])))
		if err != nil {
			return nil, err
		}
		resolved, err := read(
			int32(binary.LittleEndian.Uint32(buffer[offset+16:])),
			int32(binary.LittleEndian.Uint32(buffer[offset+20:])))
		if err != nil {
			return nil, err
		}
		extension, err := read(
			int32(binary.LittleEndian.Uint32(buffer[offset+24:])),
			int32(binary.LittleEndian.Uint32(buffer[offset+28:])))
		if err != nil {
			return nil, err
		}
		cache.Entries = append(cache.Entries, ResolutionEntry{
			FromFile:         fromFile,
			Specifier:        specifier,
			ResolvedFileName: resolved,
			Extension:        extension,
			Mode:             int32(binary.LittleEndian.Uint32(buffer[offset+32:])),
			Flags:            int32(binary.LittleEndian.Uint32(buffer[offset+36:])),
		})
	}

	directoryCursor := cursor + recordBytes
	cache.Directories = make([]string, 0, directoryCount)
	for index := 0; index < directoryCount; index++ {
		offset := directoryCursor + index*8
		directory, err := read(
			int32(binary.LittleEndian.Uint32(buffer[offset+0:])),
			int32(binary.LittleEndian.Uint32(buffer[offset+4:])))
		if err != nil {
			return nil, err
		}
		cache.Directories = append(cache.Directories, directory)
	}

	return cache, nil
}

// IsStale reports whether the filesystem shape moved since this cache was written.
//
// Returns true on any doubt. A cache wrongly treated as fresh serves stale resolutions and
// reports a clean tree; a cache wrongly treated as stale costs one slow run. Those are not
// symmetric, so every uncertain case resolves toward stale.
func (c *ResolutionCache) IsStale() bool {
	if c == nil || len(c.Directories) == 0 {
		return true
	}
	return FingerprintDirectories(c.Directories) != c.Fingerprint
}
