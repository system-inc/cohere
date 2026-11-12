package cache

import (
	"bytes"
	"compress/gzip"
	"slices"
	"testing"
)

// sampleStandardLibraryEntries are three keys, two of which share an entry, its export data and its reads, as the same
// package does under two Go patch releases, and one with no export data and no reads, as unsafe has.
func sampleStandardLibraryEntries() map[Key]StandardLibraryEntry {
	shared := StandardLibraryEntry{Entry: []byte("strings, its facts"), ExportData: []byte("strings, its types"),
		Reads: []byte("strings, its reads")}
	return map[Key]StandardLibraryEntry{
		{0x01}: shared,
		{0x02}: {Entry: bytes.Clone(shared.Entry), ExportData: bytes.Clone(shared.ExportData),
			Reads: bytes.Clone(shared.Reads)},
		{0x03}: {Entry: []byte("unsafe, its facts")},
	}
}

// A table written is read back key for key, an entry without export data or reads among them, and a key it was not
// written with is not held. The same entries always write the same bytes, and an entry two keys share is held once: the
// table of both keys is no bigger than the table of one by more than the second key's row.
func TestAStandardLibraryTableReadsBackWhatItWasWrittenWith(test *testing.T) {
	test.Parallel()

	entries := sampleStandardLibraryEntries()
	written := WriteStandardLibraryTable(entries, nil)
	table := NewStandardLibraryTable(written)
	for key, want := range entries {
		got, held := table.Lookup(key)
		if !held || !bytes.Equal(got.Entry, want.Entry) || !bytes.Equal(got.ExportData, want.ExportData) ||
			!bytes.Equal(got.Reads, want.Reads) {
			test.Errorf("key %s: want %q, %q and %q, got %q, %q and %q (held %t)", key, want.Entry, want.ExportData,
				want.Reads, got.Entry, got.ExportData, got.Reads, held)
		}
		if want.ExportData == nil && got.ExportData != nil || want.Reads == nil && got.Reads != nil {
			test.Errorf("key %s: want no export data and no reads, got %q and %q", key, got.ExportData, got.Reads)
		}
	}
	if _, held := table.Lookup(Key{0x04}); held {
		test.Error("want a key the table was not written with not held")
	}
	if problem := table.Problem(); problem != nil {
		test.Errorf("want a table that parses, got %v", problem)
	}
	if again := WriteStandardLibraryTable(sampleStandardLibraryEntries(), nil); !bytes.Equal(again, written) {
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
	// A row is the key, and three one-byte positions.
	if both, single := plainSize(WriteStandardLibraryTable(map[Key]StandardLibraryEntry{{0x01}: entries[Key{0x01}],
		{0x02}: entries[Key{0x02}]}, nil)), plainSize(WriteStandardLibraryTable(one, nil)); both !=
		single+len(Key{})+3 {
		test.Errorf("want the shared entry held once, a row of %d bytes more, got %d bytes for one key and %d for "+
			"both", len(Key{})+3, single, both)
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
	written := WriteStandardLibraryTable(sampleStandardLibraryEntries(), nil)
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
		// No variants, no contents, and one key whose entry is the first content.
		"past its contents": gzipped(standardLibraryTableMagic + "\x00\x00\x01" + string(make([]byte, len(Key{}))) +
			"\x00\x00\x00"),
		// No variants, one content, and one key whose reads are past it.
		"reads past its contents": gzipped(standardLibraryTableMagic + "\x00\x01\x01e\x01" +
			string(make([]byte, len(Key{}))) + "\x00\x00\x02"),
		// No variants, no contents, no keys, and then a byte.
		"with a byte after its keys": gzipped(standardLibraryTableMagic + "\x00\x00\x00x"),
		// One variant whose target says it is longer than the table.
		"inside a variant": gzipped(standardLibraryTableMagic + "\x01\x7f"),
		// The first format, which held no variants, and the second, which held no reads.
		"of the first format":  gzipped("cohere standard library table 1\n\x00\x00"),
		"of the second format": gzipped("cohere standard library table 2\n\x00\x00\x00"),
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

// A table says which variants it holds, in one order whatever order it was written with, and its keys in theirs: the
// same entries and variants always write the same bytes, and a table that holds nothing holds no variant and no key.
func TestAStandardLibraryTableSaysWhichVariantsItHolds(test *testing.T) {
	test.Parallel()

	variants := []StandardLibraryVariant{
		{Target: "linux/amd64", CgoEnabled: "1", GoVersion: "go1.27.1", Packages: 329},
		{Target: "darwin/arm64", CgoEnabled: "1", GoVersion: "go1.27.0", Packages: 328},
		{Target: "darwin/arm64", CgoEnabled: "0", GoVersion: "go1.27.0", Packages: 327},
	}
	written := WriteStandardLibraryTable(sampleStandardLibraryEntries(), variants)
	table := NewStandardLibraryTable(written)
	want := []StandardLibraryVariant{variants[2], variants[1], variants[0]}
	if got := table.Variants(); !slices.Equal(got, want) {
		test.Errorf("want the variants in order\n%+v\ngot\n%+v", want, got)
	}
	if got := table.Keys(); !slices.Equal(got, []Key{{0x01}, {0x02}, {0x03}}) {
		test.Errorf("want the three keys in order, got %v", got)
	}
	reversed := slices.Clone(variants)
	slices.Reverse(reversed)
	if again := WriteStandardLibraryTable(sampleStandardLibraryEntries(), reversed); !bytes.Equal(again, written) {
		test.Error("want the same variants in another order to write the same bytes")
	}
	if withoutVariants := WriteStandardLibraryTable(sampleStandardLibraryEntries(), nil); bytes.Equal(
		withoutVariants, written) {
		test.Error("want the variants among the bytes")
	}
	empty := NewStandardLibraryTable(nil)
	if len(empty.Variants()) > 0 || len(empty.Keys()) > 0 {
		test.Errorf("want an empty table to hold no variant and no key, got %+v and %v", empty.Variants(), empty.Keys())
	}
}
