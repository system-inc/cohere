package program

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// The content pack: the bytes of every file recent runs read, kept in one file beside the cache table, so
// a build serves an unchanged file from memory instead of opening it (#j9d5nm6).
//
// Opening is what it saves. On macOS, open() costs about 20 to 40us and the kernel runs them one at a
// time, so the program's 10,270 files on ahra took 130 to 250ms of wall to open whether one loader asked
// or sixteen did, a third to a half of the graph phase. stat costs 3 to 6us and does run in parallel: the
// same files take about 10ms across sixteen loaders (#zqsdzbq, lever 8).
//
// # Why a served file is the file
//
// An entry is served only when a stat taken now matches the one taken before its bytes were read, in all
// of size, modification time, change time, inode and device. The modification time alone is what the run
// cache trusts, but this cache hands the compiler its source text rather than deciding whether to replay,
// and userland can restore a modification time (cp -p, rsync -t, touch -r) over different bytes of the same
// size. Nothing in userland can set a change time, and a file replaced by a rename has a new inode. The
// stat is taken before the read, never after, so a file that changes while it is read leaves an entry
// keyed older than its bytes, which the next run's stat cannot match.
//
// # Why a damaged pack costs a read, never a byte
//
// Every entry carries a CRC-32C of its bytes, checked before it is served, and its bounds are checked
// against the mapping. The check runs with faults turned into panics, so a pack truncated in place under
// the mapping before an entry is served is a read from disk rather than a crash. The index carries a
// checksum of itself. Anything that fails is read from disk, counted, and the pack is rewritten without it.
//
// # Why a served file is a view of the mapping, not a copy
//
// A served entry is a string over the mapped bytes (#kdee854, 1b): copying them was 76 MB and about 11,300
// allocations on every ahra run that built a graph, and 8.5ms of serving. Three things make the view safe.
// The mapping is never unmapped, so it outlives every string over it and every substring the compiler and
// the rules cut from those. The bytes under it never change: the pack's writers append past a data file's
// end or write a new generation and unlink the old, and an unlinked file stays whole under its mapping. And
// it is mapped read-only, so nothing in the process can write through it.
//
// What none of that covers is another process truncating or rewriting a data file in place after an entry
// was served. The bytes are then read by the compiler's and the rules' goroutines, where no fault can be
// turned into a panic, so the run ends with a memory fault. That is loud and never a wrong answer, and the
// pack lives in .cache/cohere, which only cohere writes, and only by appending and renaming.
//
// # Layout
//
// contents.index names the data file it describes and lists each entry: path, offset, length, the stat it
// was keyed on, and its checksum. The data file, contents-<generation>.pack, only ever grows: a run that
// read files the pack lacked appends them and writes a new index by rename. When dead bytes outweigh live
// ones, or the index holds more than twice what a run used, the next writing run copies what it used into
// a new data file of a new generation, and the old one is removed after the index names the new one. A
// reader that mapped the old one keeps reading it, since an unlinked file stays whole under its mapping.

const (
	contentPackIndexName = "contents.index"
	contentPackMagic     = "COHERECP"
	contentPackVersion   = 1
)

// castagnoli is CRC-32C, which arm64 and amd64 compute in hardware.
var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// fileIdentity is what a stat says about a file's current bytes.
type fileIdentity struct {
	size                int64
	modifiedNanoseconds int64
	changedNanoseconds  int64
	inode               uint64
	device              uint64
}

type contentPackEntry struct {
	identity fileIdentity
	offset   int64
	length   int64
	checksum uint32
}

type addedContent struct {
	identity fileIdentity
	contents string
}

// ContentPack serves file bytes recorded by earlier runs and collects what this run read from disk, for
// Save to keep. Safe for the program loader's concurrent reads.
type ContentPack struct {
	directory string

	// known is the run-cache check's stats, trusted in place of a stat of each file served. Nil when the run had
	// no check, or the check ran before the pack was handed its snapshot. See StatSnapshot.
	known *StatSnapshot

	// dataName, mapped and entries are what was on disk when the pack was opened, read-only afterward.
	dataName string
	mapped   []byte
	entries  map[string]contentPackEntry

	mutex  sync.Mutex
	used   map[string]bool
	added  map[string]addedContent
	hits   int
	misses int

	// damaged counts entries that matched their stat but failed their bounds or checksum, each read from
	// disk instead. Any makes Save rewrite the pack from scratch.
	damaged int
}

// OpenContentPack reads the pack kept in directory. It always returns a usable pack: one with nothing in
// it when there is none yet, and when there is one it cannot trust, alongside an error saying why.
func OpenContentPack(directory string) (*ContentPack, error) {
	pack := &ContentPack{
		directory: directory,
		entries:   map[string]contentPackEntry{},
		used:      map[string]bool{},
		added:     map[string]addedContent{},
	}
	index, readError := os.ReadFile(filepath.Join(directory, contentPackIndexName))
	if errors.Is(readError, os.ErrNotExist) {
		return pack, nil
	}
	if readError != nil {
		return pack, fmt.Errorf("the content pack's index could not be read: %w", readError)
	}
	dataName, entries, parseContentPackIndexError := parseContentPackIndex(index)
	if parseContentPackIndexError != nil {
		return pack, fmt.Errorf(
			"the content pack's index is damaged (%w), so every file is read from disk and a new pack is written",
			parseContentPackIndexError,
		)
	}
	mapped := mapFile(filepath.Join(directory, dataName))
	if mapped == nil && len(entries) > 0 {
		return pack, fmt.Errorf(
			"the content pack's data file %s could not be mapped, so every file is read from disk and a new pack is written",
			dataName,
		)
	}
	pack.dataName, pack.mapped, pack.entries = dataName, mapped, entries
	return pack, nil
}

// TrustStats has the pack validate entries against snapshot's stats where it holds one, rather than statting
// each file served again. See StatSnapshot for why that is sound.
func (pack *ContentPack) TrustStats(snapshot *StatSnapshot) {
	pack.known = snapshot
}

// Counts reports how many files this run was served from the pack, read from disk, and found damaged.
func (pack *ContentPack) Counts() (hits int, misses int, damaged int) {
	pack.mutex.Lock()
	defer pack.mutex.Unlock()
	return pack.hits, pack.misses, pack.damaged
}

// wrap returns a filesystem that serves ReadFile from the pack where it can.
func (pack *ContentPack) wrap(inner vfs.FS) vfs.FS {
	return &contentPackFileSystem{FS: inner, pack: pack}
}

// serve returns a file's recorded bytes when its stat still matches the entry's.
func (pack *ContentPack) serve(path string) (string, bool) {
	entry, found := pack.entries[path]
	if !found {
		return "", false
	}
	identity, statted := pack.known.identityOf(path)
	if !statted {
		identity, statted = statIdentity(path)
	}
	if !statted || identity != entry.identity {
		return "", false
	}
	contents, whole := pack.viewEntry(entry)
	pack.mutex.Lock()
	defer pack.mutex.Unlock()
	if !whole {
		pack.damaged++
		return "", false
	}
	pack.used[path] = true
	pack.hits++
	return contents, true
}

// viewEntry checks an entry's bytes in the mapping and returns them as a string over the mapping, reporting
// false for anything out of bounds, faulting or failing its checksum. See the package comment for why the
// view is safe to hand the compiler.
func (pack *ContentPack) viewEntry(entry contentPackEntry) (contents string, whole bool) {
	if entry.offset < 0 || entry.length < 0 || entry.offset > int64(len(pack.mapped))-entry.length {
		return "", false
	}
	if entry.length == 0 {
		return "", crc32.Checksum(nil, castagnoli) == entry.checksum
	}
	// A pack truncated in place under the mapping faults on the checksum, which reads every byte. Turned into
	// a panic and recovered, it is a read from disk rather than the end of the process.
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if recover() != nil {
			contents, whole = "", false
		}
	}()
	view := pack.mapped[entry.offset : entry.offset+entry.length]
	if crc32.Checksum(view, castagnoli) != entry.checksum {
		return "", false
	}
	// Unsafe: the mapping is read-only and never unmapped, and its bytes are never written in place, so the
	// string outlives nothing it points at and never changes. The package comment says why each holds.
	return unsafe.String(&view[0], len(view)), true
}

// readIdentity is the stat the program's copy of path was served or read under, when the pack has one: an entry
// it served, or a file it read whole after a stat. See Graph.ReadUnchanged.
func (pack *ContentPack) readIdentity(path string) (fileIdentity, bool) {
	pack.mutex.Lock()
	defer pack.mutex.Unlock()
	if added, read := pack.added[path]; read {
		return added.identity, true
	}
	if pack.used[path] {
		return pack.entries[path].identity, true
	}
	return fileIdentity{}, false
}

// noteRead records a file read from disk, keepable when it was statted before the read and read whole.
func (pack *ContentPack) noteRead(path string, identity fileIdentity, keepable bool, contents string) {
	pack.mutex.Lock()
	defer pack.mutex.Unlock()
	pack.misses++
	if keepable {
		pack.added[path] = addedContent{identity: identity, contents: contents}
	}
}

// Save keeps what this run read from disk, appending it to the data file or, when that has grown too dead
// or something in it was damaged, writing a new one. A run that read nothing new writes nothing. The caller
// holds the cache directory's lock, so two runs never write at once.
func (pack *ContentPack) Save() error {
	pack.mutex.Lock()
	defer pack.mutex.Unlock()
	if len(pack.added) == 0 && pack.damaged == 0 {
		return nil
	}
	if createDirectoryError := cache_directory.Make(pack.directory); createDirectoryError != nil {
		return createDirectoryError
	}

	// Read again under the lock: another run may have appended or rewritten since this one opened.
	current := map[string]contentPackEntry{}
	currentData := ""
	if index, readError := os.ReadFile(filepath.Join(pack.directory, contentPackIndexName)); readError == nil {
		if dataName, entries, parseContentPackIndexError := parseContentPackIndex(
			index,
		); parseContentPackIndexError == nil {
			currentData, current = dataName, entries
		}
	}

	appendable := pack.damaged == 0 && currentData != "" && currentData == pack.dataName
	if appendable {
		// Live and dead as they would stand after the append: an entry this run read again is dead in the
		// data file once its new copy is appended.
		live, size := int64(0), int64(0)
		for path, entry := range current {
			if _, replaced := pack.added[path]; !replaced {
				live += entry.length
			}
		}
		if information, statError := os.Stat(filepath.Join(pack.directory, currentData)); statError == nil {
			size = information.Size()
		}
		for _, added := range pack.added {
			live += int64(len(added.contents))
			size += int64(len(added.contents))
		}
		appendable = size-live <= live && len(current) <= 2*(len(pack.used)+len(pack.added))
	}
	if appendable {
		return pack.appendTo(currentData, current)
	}
	return pack.rewrite()
}

// appendTo adds this run's reads to the end of the current data file and writes an index naming them.
//
// Mutates: entries gains an entry for each file this run read, as the index written here names them
func (pack *ContentPack) appendTo(dataName string, entries map[string]contentPackEntry) error {
	file, openError := os.OpenFile(filepath.Join(pack.directory, dataName), os.O_WRONLY|os.O_APPEND, 0)
	if openError != nil {
		return pack.rewrite()
	}
	information, statError := file.Stat()
	if statError != nil {
		// Ignored: nothing was written yet, and the stat's error is the one to report.
		file.Close()
		return statError
	}
	offset := information.Size()
	writer := newPackWriter(file, offset)
	for path, added := range pack.added {
		entries[path] = writer.add(added.identity, added.contents)
	}
	if finishError := writer.finish(); finishError != nil {
		// Ignored: the write's error is the one to report, and the index never names what it left.
		file.Close()
		return finishError
	}
	if closeError := file.Close(); closeError != nil {
		return closeError
	}
	return writeContentPackIndex(pack.directory, dataName, entries)
}

// rewrite writes a new data file holding what this run used and read, names it in a new index, and removes
// every older data file.
func (pack *ContentPack) rewrite() error {
	dataName := "contents-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".pack"
	file, openError := os.OpenFile(filepath.Join(pack.directory, dataName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if openError != nil {
		return openError
	}
	writer := newPackWriter(file, 0)
	entries := make(map[string]contentPackEntry, len(pack.used)+len(pack.added))
	for path := range pack.used {
		if _, replaced := pack.added[path]; replaced {
			continue
		}
		entry := pack.entries[path]
		contents, whole := pack.viewEntry(entry)
		if !whole {
			continue
		}
		entries[path] = writer.add(entry.identity, contents)
	}
	for path, added := range pack.added {
		entries[path] = writer.add(added.identity, added.contents)
	}
	// A data file no index names is never read, so removing one on a failure below is tidying, and a removal that
	// fails leaves only space behind and the failure that matters already in hand.
	if finishError := writer.finish(); finishError != nil {
		// Ignored: the write's error is the one to report.
		file.Close()
		// Ignored: no index names this file, as above.
		os.Remove(file.Name())
		return finishError
	}
	if closeError := file.Close(); closeError != nil {
		// Ignored: no index names this file, as above.
		os.Remove(file.Name())
		return closeError
	}
	if writeContentPackIndexError := writeContentPackIndex(
		pack.directory,
		dataName,
		entries,
	); writeContentPackIndexError != nil {
		// Ignored: no index names this file, as above.
		os.Remove(file.Name())
		return writeContentPackIndexError
	}
	if older, globError := filepath.Glob(filepath.Join(pack.directory, "contents-*.pack")); globError == nil {
		for _, path := range older {
			if filepath.Base(path) != dataName {
				// Ignored: the new index names only dataName, so an older data file is never read again, and one
				// that will not go is retried by the next rewrite's sweep.
				os.Remove(path)
			}
		}
	}
	return nil
}

func newPackWriter(file *os.File, offset int64) *packWriter {
	return &packWriter{file: file, offset: offset, buffer: make([]byte, 0, 4<<20)}
}

// writeContentPackIndex writes the index by rename, so a reader sees the old one or the new one whole.
func writeContentPackIndex(directory string, dataName string, entries map[string]contentPackEntry) error {
	var index []byte
	index = append(index, contentPackMagic...)
	index = binary.LittleEndian.AppendUint32(index, contentPackVersion)
	index = binary.LittleEndian.AppendUint16(index, uint16(len(dataName)))
	index = append(index, dataName...)
	index = binary.LittleEndian.AppendUint32(index, uint32(len(entries)))
	for path, entry := range entries {
		index = binary.LittleEndian.AppendUint32(index, uint32(len(path)))
		index = append(index, path...)
		for _, value := range []uint64{uint64(entry.offset), uint64(entry.length), uint64(entry.identity.size),
			uint64(entry.identity.modifiedNanoseconds), uint64(entry.identity.changedNanoseconds), entry.identity.inode,
			entry.identity.device} {
			index = binary.LittleEndian.AppendUint64(index, value)
		}
		index = binary.LittleEndian.AppendUint32(index, entry.checksum)
	}
	index = binary.LittleEndian.AppendUint32(index, crc32.Checksum(index, castagnoli))

	temporary, temporaryFileError := os.CreateTemp(directory, contentPackIndexName+".*")
	if temporaryFileError != nil {
		return temporaryFileError
	}
	// A temporary is never read as the index, so removing one on a failure below is tidying, and a removal that
	// fails leaves only space behind and the failure that matters already in hand.
	if _, writeError := temporary.Write(index); writeError != nil {
		// Ignored: the write's error is the one to report.
		temporary.Close()
		// Ignored: a temporary is never read, as above.
		os.Remove(temporary.Name())
		return writeError
	}
	if closeError := temporary.Close(); closeError != nil {
		// Ignored: a temporary is never read, as above.
		os.Remove(temporary.Name())
		return closeError
	}
	if renameError := os.Rename(temporary.Name(), filepath.Join(directory, contentPackIndexName)); renameError != nil {
		// Ignored: a temporary is never read, as above.
		os.Remove(temporary.Name())
		return renameError
	}
	return nil
}

// parseContentPackIndex reads an index, refusing anything whose checksum, version or lengths do not hold.
func parseContentPackIndex(index []byte) (string, map[string]contentPackEntry, error) {
	if len(index) < len(contentPackMagic)+4 {
		return "", nil, errors.New("too short")
	}
	body, trailer := index[:len(index)-4], index[len(index)-4:]
	if crc32.Checksum(body, castagnoli) != binary.LittleEndian.Uint32(trailer) {
		return "", nil, errors.New("its checksum does not match")
	}
	reader := &indexReader{data: body}
	if string(reader.bytes(len(contentPackMagic))) != contentPackMagic {
		return "", nil, errors.New("not a content pack index")
	}
	if version := reader.uint32(); version != contentPackVersion {
		return "", nil, fmt.Errorf("format %d, this build reads %d", version, contentPackVersion)
	}
	dataName := string(reader.bytes(int(reader.uint16())))
	if dataName == "" || strings.ContainsAny(dataName, `/\`) {
		return "", nil, fmt.Errorf("it names %q as its data file", dataName)
	}
	count := int(reader.uint32())
	entries := make(map[string]contentPackEntry, min(count, len(body)/64))
	for range count {
		path := string(reader.bytes(int(reader.uint32())))
		var values [7]uint64
		for index := range values {
			values[index] = reader.uint64()
		}
		entries[path] = contentPackEntry{
			offset: int64(values[0]),
			length: int64(values[1]),
			identity: fileIdentity{size: int64(values[2]), modifiedNanoseconds: int64(values[3]),
				changedNanoseconds: int64(values[4]), inode: values[5], device: values[6]},
			checksum: reader.uint32(),
		}
	}
	if reader.readError != nil {
		return "", nil, reader.readError
	}
	if len(reader.data) != reader.position {
		return "", nil, errors.New("bytes follow its last entry")
	}
	return dataName, entries, nil
}
