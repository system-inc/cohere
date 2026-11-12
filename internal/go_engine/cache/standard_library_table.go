package cache

import (
	"bytes"
	"cmp"
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
const standardLibraryTableMagic = "cohere standard library table 3\n"

// StandardLibraryEntry is one standard-library package as a table holds it: its cache entry as the checker encodes
// it, its export data, nil for unsafe, whose types are the type checker's own, and its reads of other packages as the
// checker persists them, nil for a package whose reads could not be persisted, which an extra build declines to restore
// from the host (#pcv6eff). An entry read from a table shares the table's bytes, which nothing may change.
type StandardLibraryEntry struct {
	Entry      []byte
	ExportData []byte
	Reads      []byte
}

// StandardLibraryVariant is one build a table holds the standard library of: the target, `GOOS/GOARCH`, the
// `CGO_ENABLED` value, the Go version as go env reports it, and how many packages the release computed under it. A
// key names none of these readably, since it is a digest, so the table says which builds it holds beside its keys,
// and the release's check recomputes exactly those.
type StandardLibraryVariant struct {
	Target     string
	CgoEnabled string
	GoVersion  string
	Packages   int
}

// compareVariants orders two variants by target, Go version, then `CGO_ENABLED`, the order a table writes them in.
func compareVariants(left StandardLibraryVariant, right StandardLibraryVariant) int {
	return cmp.Or(cmp.Compare(left.Target, right.Target), cmp.Compare(left.GoVersion, right.GoVersion),
		cmp.Compare(left.CgoEnabled, right.CgoEnabled), cmp.Compare(left.Packages, right.Packages))
}

// StandardLibraryTable is the standard library's entries a release builds into the binary, read-only, by each package's
// StandardLibraryKeys key. It is content-addressed: an entry, export data or reads that several keys share, as most do
// across Go's patch releases and across `CGO_ENABLED`, are held once. It is parsed whole on the first Lookup and never
// again. An empty table holds nothing, and so does one that does not parse, which Problem then says: a table never
// crashes a run and never changes a finding, since every package it does not hold is computed.
//
// The table is gzip, which Go's standard library reads and whose checksum catches bytes changed in it. Inside, after
// standardLibraryTableMagic, are the variants, the contents and then the index, every number an unsigned varint and
// every text its length and its bytes: the count of the variants, then each one as its target, `CGO_ENABLED`, Go
// version and package count, in compareVariants' order; the count of the contents, then each one as its length and its
// bytes, in the order of their sha256, no two alike; then the count of the keys, then each key as its 32 bytes, the
// position of its entry among the contents, the position of its export data plus one, 0 for none, and the position of
// its reads plus one, 0 for none, in the order of the keys.
type StandardLibraryTable struct {
	contents []byte

	parseOnce  sync.Once
	entries    map[Key]StandardLibraryEntry
	variants   []StandardLibraryVariant
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

// Variants are the builds the table holds the standard library of, in compareVariants' order, none when it holds
// nothing.
func (table *StandardLibraryTable) Variants() []StandardLibraryVariant {
	table.parseOnce.Do(table.parse)
	return slices.Clone(table.variants)
}

// Keys are every key the table holds an entry under, in their order, none when it holds nothing.
func (table *StandardLibraryTable) Keys() []Key {
	table.parseOnce.Do(table.parse)
	return slices.SortedFunc(maps.Keys(table.entries), compareKeys)
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
	entries, variants, parseError := parseStandardLibraryTable(table.contents)
	if parseError != nil {
		table.parseError = fmt.Errorf("reading the standard library's table: %w", parseError)
		return
	}
	table.entries, table.variants = entries, variants
}

// errorTableShort is a table that ends inside what it says comes next.
var errorTableShort = errors.New("it ends early")

// parseStandardLibraryTable is the entries a table's bytes hold, each sharing the decompressed bytes, and the variants
// it says it holds.
func parseStandardLibraryTable(compressed []byte) (map[Key]StandardLibraryEntry, []StandardLibraryVariant, error) {
	reader, gzipError := gzip.NewReader(bytes.NewReader(compressed))
	if gzipError != nil {
		return nil, nil, gzipError
	}
	plain, readError := io.ReadAll(reader)
	if readError != nil {
		return nil, nil, readError
	}
	rest, isTable := bytes.CutPrefix(plain, []byte(standardLibraryTableMagic))
	if !isTable {
		return nil, nil, errors.New("it does not open as a table of this format")
	}
	next := func() (uint64, error) {
		value, size := binary.Uvarint(rest)
		if size <= 0 {
			return 0, errorTableShort
		}
		rest = rest[size:]
		return value, nil
	}
	nextBytes := func() ([]byte, error) {
		length, lengthError := next()
		if lengthError != nil {
			return nil, lengthError
		}
		if length > uint64(len(rest)) {
			return nil, errorTableShort
		}
		read := rest[:length:length]
		rest = rest[length:]
		return read, nil
	}
	variantCount, variantCountError := next()
	if variantCountError != nil {
		return nil, nil, variantCountError
	}
	variants := make([]StandardLibraryVariant, 0, min(variantCount, uint64(len(rest))))
	for range variantCount {
		var texts [3][]byte
		for index := range texts {
			text, textError := nextBytes()
			if textError != nil {
				return nil, nil, textError
			}
			texts[index] = text
		}
		packages, packagesError := next()
		if packagesError != nil {
			return nil, nil, packagesError
		}
		variants = append(variants, StandardLibraryVariant{Target: string(texts[0]), CgoEnabled: string(texts[1]),
			GoVersion: string(texts[2]), Packages: int(packages)})
	}
	contentCount, countError := next()
	if countError != nil {
		return nil, nil, countError
	}
	contents := make([][]byte, 0, min(contentCount, uint64(len(rest))))
	for range contentCount {
		content, contentError := nextBytes()
		if contentError != nil {
			return nil, nil, contentError
		}
		contents = append(contents, content)
	}
	keyCount, keyCountError := next()
	if keyCountError != nil {
		return nil, nil, keyCountError
	}
	entries := make(map[Key]StandardLibraryEntry, min(keyCount, uint64(len(rest))))
	for range keyCount {
		if len(rest) < len(Key{}) {
			return nil, nil, errorTableShort
		}
		key := Key(rest[:len(Key{})])
		rest = rest[len(Key{}):]
		entryIndex, entryError := next()
		if entryError != nil {
			return nil, nil, entryError
		}
		exportIndex, exportError := next()
		if exportError != nil {
			return nil, nil, exportError
		}
		readsIndex, readsError := next()
		if readsError != nil {
			return nil, nil, readsError
		}
		if entryIndex >= uint64(len(contents)) || exportIndex > uint64(len(contents)) ||
			readsIndex > uint64(len(contents)) {
			return nil, nil, fmt.Errorf("the key %s names content past the %d it holds", key, len(contents))
		}
		entry := StandardLibraryEntry{Entry: contents[entryIndex]}
		if exportIndex > 0 {
			entry.ExportData = contents[exportIndex-1]
		}
		if readsIndex > 0 {
			entry.Reads = contents[readsIndex-1]
		}
		entries[key] = entry
	}
	if len(rest) > 0 {
		return nil, nil, fmt.Errorf("%d bytes follow its last key", len(rest))
	}
	return entries, variants, nil
}

// WriteStandardLibraryTable is the table that holds entries and says it holds variants, in the format
// StandardLibraryTable reads. The same entries and variants always give the same bytes: the variants go in
// compareVariants' order, the contents in the order of their sha256, the keys in their own order, and gzip's header
// holds no name and no time.
func WriteStandardLibraryTable(entries map[Key]StandardLibraryEntry, variants []StandardLibraryVariant) []byte {
	byDigest := map[Key][]byte{}
	for _, entry := range entries {
		byDigest[sha256.Sum256(entry.Entry)] = entry.Entry
		if entry.ExportData != nil {
			byDigest[sha256.Sum256(entry.ExportData)] = entry.ExportData
		}
		if entry.Reads != nil {
			byDigest[sha256.Sum256(entry.Reads)] = entry.Reads
		}
	}
	digests := slices.SortedFunc(maps.Keys(byDigest), compareKeys)
	position := make(map[Key]uint64, len(digests))
	var plain bytes.Buffer
	plain.WriteString(standardLibraryTableMagic)
	sortedVariants := slices.SortedFunc(slices.Values(variants), compareVariants)
	plain.Write(binary.AppendUvarint(nil, uint64(len(sortedVariants))))
	for _, variant := range sortedVariants {
		for _, text := range []string{variant.Target, variant.CgoEnabled, variant.GoVersion} {
			plain.Write(binary.AppendUvarint(nil, uint64(len(text))))
			plain.WriteString(text)
		}
		plain.Write(binary.AppendUvarint(nil, uint64(variant.Packages)))
	}
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
		readsPosition := uint64(0)
		if entries[key].Reads != nil {
			readsPosition = position[sha256.Sum256(entries[key].Reads)] + 1
		}
		plain.Write(binary.AppendUvarint(nil, readsPosition))
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
