package cache

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// sampleStandardLibraryEntries are three keys, two of which share an entry and its export data, as the same package
// does under two Go patch releases, and one with no export data, as unsafe has.
func sampleStandardLibraryEntries() map[Key]StandardLibraryEntry {
	shared := StandardLibraryEntry{Entry: []byte("strings, its facts"), ExportData: []byte("strings, its types")}
	return map[Key]StandardLibraryEntry{
		{0x01}: shared,
		{0x02}: {Entry: bytes.Clone(shared.Entry), ExportData: bytes.Clone(shared.ExportData)},
		{0x03}: {Entry: []byte("unsafe, its facts")},
	}
}

// A table written is read back key for key, an entry without export data among them, and a key it was not written
// with is not held. The same entries always write the same bytes, and an entry two keys share is held once: the table
// of both keys is no bigger than the table of one by more than the second key's row.
func TestAStandardLibraryTableReadsBackWhatItWasWrittenWith(test *testing.T) {
	test.Parallel()

	entries := sampleStandardLibraryEntries()
	written := WriteStandardLibraryTable(entries)
	table := NewStandardLibraryTable(written)
	for key, want := range entries {
		got, held := table.Lookup(key)
		if !held || !bytes.Equal(got.Entry, want.Entry) || !bytes.Equal(got.ExportData, want.ExportData) {
			test.Errorf("key %s: want %q and %q, got %q and %q (held %t)", key, want.Entry, want.ExportData,
				got.Entry, got.ExportData, held)
		}
		if want.ExportData == nil && got.ExportData != nil {
			test.Errorf("key %s: want no export data, got %q", key, got.ExportData)
		}
	}
	if _, held := table.Lookup(Key{0x04}); held {
		test.Error("want a key the table was not written with not held")
	}
	if problem := table.Problem(); problem != nil {
		test.Errorf("want a table that parses, got %v", problem)
	}
	if again := WriteStandardLibraryTable(sampleStandardLibraryEntries()); !bytes.Equal(again, written) {
		test.Error("want the same entries to write the same bytes")
	}
	plainSize := func(compressed []byte) int {
		test.Helper()
		reader, readerError := gzip.NewReader(bytes.NewReader(compressed))
		if readerError != nil {
			test.Fatal(readerError)
		}
		var plain bytes.Buffer
		if _, readError := plain.ReadFrom(reader); readError != nil {
			test.Fatal(readError)
		}
		return plain.Len()
	}
	one := map[Key]StandardLibraryEntry{{0x01}: entries[Key{0x01}]}
	// A row is the key, and two one-byte positions.
	if both, single := plainSize(WriteStandardLibraryTable(map[Key]StandardLibraryEntry{{0x01}: entries[Key{0x01}],
		{0x02}: entries[Key{0x02}]})), plainSize(WriteStandardLibraryTable(one)); both != single+len(Key{})+2 {
		test.Errorf("want the shared entry held once, a row of %d bytes more, got %d bytes for one key and %d for "+
			"both", len(Key{})+2, single, both)
	}
}

// An empty table holds nothing and has no problem, and is told empty without parsing; a table that does not parse
// holds nothing and says why, whether its bytes are no gzip, a gzip cut short, gzip of something else, or a table
// whose index names content it does not hold.
func TestAStandardLibraryTableThatDoesNotParseHoldsNothing(test *testing.T) {
	test.Parallel()

	empty := NewStandardLibraryTable(nil)
	if _, held := empty.Lookup(Key{0x01}); held || empty.Problem() != nil || !empty.Empty() {
		test.Errorf("want an empty table holding nothing with no problem, got problem %v", empty.Problem())
	}
	written := WriteStandardLibraryTable(sampleStandardLibraryEntries())
	gzipped := func(plain string) []byte {
		test.Helper()
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, writeError := writer.Write([]byte(plain)); writeError != nil {
			test.Fatal(writeError)
		}
		if closeError := writer.Close(); closeError != nil {
			test.Fatal(closeError)
		}
		return compressed.Bytes()
	}
	for name, contents := range map[string][]byte{
		"no gzip":           []byte("not a table"),
		"cut short":         written[:len(written)/2],
		"of another format": gzipped("some other table\n"),
		// No contents, and one key whose entry is the first content.
		"past its contents": gzipped(standardLibraryTableMagic + "\x00\x01" + string(make([]byte, len(Key{}))) +
			"\x00\x00"),
		// No contents, no keys, and then a byte.
		"with a byte after its keys": gzipped(standardLibraryTableMagic + "\x00\x00x"),
	} {
		table := NewStandardLibraryTable(contents)
		if table.Empty() {
			test.Errorf("%s: want a table with bytes not empty", name)
		}
		if _, held := table.Lookup(Key{0x01}); held {
			test.Errorf("%s: want nothing held", name)
		}
		if table.Problem() == nil {
			test.Errorf("%s: want the problem said", name)
		}
	}
}
