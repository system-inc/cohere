package check

import (
	"bytes"
	"encoding/gob"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// storedRecordAt is the record the store keeps under key, failing the test when it holds none that decodes.
func storedRecordAt(test *testing.T, store *cache.Store, key cache.Key) entryRecord {
	test.Helper()
	contents, stored := store.Get(key)
	if !stored {
		test.Fatalf("the store holds no record under %s", key)
	}
	record, decodeError := decodeRecord(contents)
	if decodeError != nil {
		test.Fatalf("decoding the record under %s: %v", key, decodeError)
	}
	return record
}

// decodedStoredEntry is restoring's entry the store keeps under key, decoded as a hit decodes it.
func decodedStoredEntry(
	test *testing.T,
	store *cache.Store,
	known *cacheAnalyzers,
	restoring *packages.Package,
	key cache.Key,
) (*restoredPackage, error) {
	test.Helper()
	contents, stored := store.Get(key)
	if !stored {
		test.Fatalf("the store holds no record of %s", restoring.ID)
	}
	body, record, whole := storedEntry(store, contents)
	if !whole {
		test.Fatalf("the store holds no body of %s, which its record holds or names", restoring.ID)
	}
	return decodeEntry(known, restoring, body, record, nil)
}

// rewriteStoredEntry changes the entry the store keeps under key, as change leaves it, and writes it back: its record
// holding the body changed, its export data and reads as they were.
func rewriteStoredEntry(test *testing.T, store *cache.Store, key cache.Key, change func(entry *cacheEntry)) {
	test.Helper()
	entry, record, stored := storedEntryAt(store, key)
	if !stored {
		test.Fatalf("the store holds no whole entry under %s", key)
	}
	change(&entry)
	body, encodeError := encodeBody(entry)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	record.Body, record.Inline, record.BodyAt = contentDigest(body), body, nil
	encoded, recordError := encodeRecord(record)
	if recordError != nil {
		test.Fatal(recordError)
	}
	if putError := store.Put(key, encoded); putError != nil {
		test.Fatal(putError)
	}
}

// decodeWholeEntry is a whole entry's gob, body and reads in one, as the standard library's table is written from,
// decoded as a hit decodes one.
func decodeWholeEntry(known *cacheAnalyzers, restoring *packages.Package, whole []byte) (*restoredPackage, error) {
	var entry cacheEntry
	if decodeError := gob.NewDecoder(bytes.NewReader(whole)).Decode(&entry); decodeError != nil {
		return nil, decodeError
	}
	return restoredOf(known, restoring, entry, nil)
}
