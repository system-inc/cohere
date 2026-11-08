package cache

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
)

// standardLibraryTableMagic opens every table, and names its format, so bytes of another format, or of none, are a
// table that does not parse rather than entries read wrong.
const standardLibraryTableMagic = "cohere standard library table 1\n"

// StandardLibraryEntry is one standard-library package as a table holds it: its cache entry as the checker encodes
// it, and its export data, nil for unsafe, whose types are the type checker's own. An entry read from a table shares
// the table's bytes, which nothing may change.
type StandardLibraryEntry struct {
	Entry      []byte
	ExportData []byte
}

// StandardLibraryTable is the standard library's entries a release builds into the binary, read-only, by each
// package's StandardLibraryKeys key. It is content-addressed: an entry or export data that several keys share, as
// most do across Go's patch releases and across `CGO_ENABLED`, is held once. It is parsed whole on the first Lookup
// and never again. An empty table holds nothing, and so does one that does not parse, which Problem then says: a
// table never crashes a run and never changes a finding, since every package it does not hold is computed.
//
// The table is gzip, which Go's standard library reads and whose checksum catches bytes changed in it. Inside, after
// standardLibraryTableMagic, are the contents and then the index, every number an unsigned varint: the count of the
// contents, then each one as its length and its bytes, in the order of their sha256, no two alike; then the count of
// the keys, then each key as its 32 bytes, the position of its entry among the contents, and the position of its
// export data plus one, 0 for none, in the order of the keys.
type StandardLibraryTable struct {
	contents []byte

	parseOnce  sync.Once
	entries    map[Key]StandardLibraryEntry
	parseError error
}

// NewStandardLibraryTable is the table contents hold, as WriteStandardLibraryTable writes one, unparsed.
func NewStandardLibraryTable(contents []byte) *StandardLibraryTable {
	return &StandardLibraryTable{contents: contents}
}

// Empty reports whether the table has no bytes at all, as a build that is no release embeds, without parsing it.
func (table *StandardLibraryTable) Empty() bool {
	return len(table.contents) == 0
}

// Lookup is the entry the table holds under key, and false when it holds none, or does not parse.
func (table *StandardLibraryTable) Lookup(key Key) (StandardLibraryEntry, bool) {
	table.parseOnce.Do(table.parse)
	entry, held := table.entries[key]
	return entry, held
}

// Problem is why the table holds nothing though it has bytes, nil when it parsed or has none. It parses the table
// when no Lookup has yet.
func (table *StandardLibraryTable) Problem() error {
	table.parseOnce.Do(table.parse)
	return table.parseError
}

// parse reads the table's bytes into entries, or leaves it holding nothing with the reason in parseError.
func (table *StandardLibraryTable) parse() {
	if len(table.contents) == 0 {
		return
	}
	entries, parseError := parseStandardLibraryTable(table.contents)
	if parseError != nil {
		table.parseError = fmt.Errorf("reading the standard library's table: %w", parseError)
		return
	}
	table.entries = entries
}

// errorTableShort is a table that ends inside what it says comes next.
var errorTableShort = errors.New("it ends early")

// parseStandardLibraryTable is the entries a table's bytes hold, each sharing the decompressed bytes.
func parseStandardLibraryTable(compressed []byte) (map[Key]StandardLibraryEntry, error) {
	reader, gzipError := gzip.NewReader(bytes.NewReader(compressed))
	if gzipError != nil {
		return nil, gzipError
	}
	plain, readError := io.ReadAll(reader)
	if readError != nil {
		return nil, readError
	}
	rest, isTable := bytes.CutPrefix(plain, []byte(standardLibraryTableMagic))
	if !isTable {
		return nil, errors.New("it does not open as a table of this format")
	}
	next := func() (uint64, error) {
		value, size := binary.Uvarint(rest)
		if size <= 0 {
			return 0, errorTableShort
		}
		rest = rest[size:]
		return value, nil
	}
	contentCount, countError := next()
	if countError != nil {
		return nil, countError
	}
	contents := make([][]byte, 0, min(contentCount, uint64(len(rest))))
	for range contentCount {
		length, lengthError := next()
		if lengthError != nil {
			return nil, lengthError
		}
		if length > uint64(len(rest)) {
			return nil, errorTableShort
		}
		contents = append(contents, rest[:length:length])
		rest = rest[length:]
	}
	keyCount, keyCountError := next()
	if keyCountError != nil {
		return nil, keyCountError
	}
	entries := make(map[Key]StandardLibraryEntry, min(keyCount, uint64(len(rest))))
	for range keyCount {
		if len(rest) < len(Key{}) {
			return nil, errorTableShort
		}
		key := Key(rest[:len(Key{})])
		rest = rest[len(Key{}):]
		entryIndex, entryError := next()
		if entryError != nil {
			return nil, entryError
		}
		exportIndex, exportError := next()
		if exportError != nil {
			return nil, exportError
		}
		if entryIndex >= uint64(len(contents)) || exportIndex > uint64(len(contents)) {
			return nil, fmt.Errorf("the key %s names content past the %d it holds", key, len(contents))
		}
		entry := StandardLibraryEntry{Entry: contents[entryIndex]}
		if exportIndex > 0 {
			entry.ExportData = contents[exportIndex-1]
		}
		entries[key] = entry
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%d bytes follow its last key", len(rest))
	}
	return entries, nil
}

// WriteStandardLibraryTable is the table that holds entries, in the format StandardLibraryTable reads. The same
// entries always give the same bytes: the contents go in the order of their sha256, the keys in their own order, and
// gzip's header holds no name and no time.
func WriteStandardLibraryTable(entries map[Key]StandardLibraryEntry) []byte {
	byDigest := map[Key][]byte{}
	for _, entry := range entries {
		byDigest[sha256.Sum256(entry.Entry)] = entry.Entry
		if entry.ExportData != nil {
			byDigest[sha256.Sum256(entry.ExportData)] = entry.ExportData
		}
	}
	digests := slices.SortedFunc(maps.Keys(byDigest), compareKeys)
	position := make(map[Key]uint64, len(digests))
	var plain bytes.Buffer
	plain.WriteString(standardLibraryTableMagic)
	plain.Write(binary.AppendUvarint(nil, uint64(len(digests))))
	for index, digest := range digests {
		position[digest] = uint64(index)
		plain.Write(binary.AppendUvarint(nil, uint64(len(byDigest[digest]))))
		plain.Write(byDigest[digest])
	}
	keys := slices.SortedFunc(maps.Keys(entries), compareKeys)
	plain.Write(binary.AppendUvarint(nil, uint64(len(keys))))
	for _, key := range keys {
		plain.Write(key[:])
		plain.Write(binary.AppendUvarint(nil, position[sha256.Sum256(entries[key].Entry)]))
		exportPosition := uint64(0)
		if entries[key].ExportData != nil {
			exportPosition = position[sha256.Sum256(entries[key].ExportData)] + 1
		}
		plain.Write(binary.AppendUvarint(nil, exportPosition))
	}
	var compressed bytes.Buffer
	// Ignored: the level is one gzip knows, so it returns no error.
	writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	// Ignored: gzip writes to a bytes.Buffer, whose Write never fails.
	_, _ = writer.Write(plain.Bytes())
	// Ignored: gzip writes to a bytes.Buffer, whose Write never fails.
	_ = writer.Close()
	return compressed.Bytes()
}

// compareKeys orders two keys, or two digests, by their bytes.
func compareKeys(left Key, right Key) int {
	return bytes.Compare(left[:], right[:])
}
