// Package system_cache reads the engine's system cache as the fork's types (#hmepe1g): the shallow export data the
// engine wrote of each standard-library package it checked, one entry a build, read into one build's universe at a
// time, so the one walk's merging importer takes what the engine already has rather than ask go for compiler export
// data. It reads the engine's store and never writes it: a file not whole is a miss here, where the engine's own
// reader removes it, and nothing is marked used.
//
// A package's entry is found by its key in its build, which the engine's session computes (cache.StandardLibraryKeys)
// from what this package cannot see: the engine binary's identity, the run's fact analyzers and their options, the
// build's go env and every file's bytes. So the keys are the caller's to give, a build at a time.
package system_cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
)

// Store is the engine's store in Directory, the system cache's `<root>/go/store` or a project's `.cache/cohere/go`,
// read and never written.
type Store struct {
	Directory string
}

// Get is what the store holds under key, the file's bytes after the sha256 that heads them, as cache.Store.Get reads
// them, and false for a file missing or not whole, which is left as it is.
func (store Store) Get(key cache.Key) ([]byte, bool) {
	contents, readError := os.ReadFile(filepath.Join(store.Directory, key.String()[:2], key.String()))
	if readError != nil || len(contents) < sha256.Size {
		return nil, false
	}
	entry := contents[sha256.Size:]
	sum := sha256.Sum256(entry)
	if !bytes.Equal(contents[:sha256.Size], sum[:]) {
		return nil, false
	}
	return entry, true
}

// entryRecord is what this package reads of the record the store keeps under a package's key, check's entryRecord, of
// which gob decodes these two fields by name and skips the rest: the format it was written in, and the sha256 of the
// package's export data.
type entryRecord struct {
	Version    string
	ExportData []byte
}

// exportDataKey is where the store keeps the export data whose sha256 is digest, as check's exportDataKey makes it.
func exportDataKey(digest []byte) cache.Key {
	return sha256.Sum256(append([]byte("export data by content "), digest...))
}

// ExportData is the export data the store holds for the package whose key is key, and false when it holds no record
// there, a record of another format, one that names no export data, or no export data by the name it gives: each a
// package the store does not have for this engine, never an error.
func (store Store) ExportData(key cache.Key) ([]byte, bool) {
	contents, stored := store.Get(key)
	if !stored {
		return nil, false
	}
	var record entryRecord
	if gob.NewDecoder(bytes.NewReader(contents)).Decode(&record) != nil || record.Version != check.CacheFormatVersion ||
		record.ExportData == nil {
		return nil, false
	}
	return store.Get(exportDataKey(record.ExportData))
}
