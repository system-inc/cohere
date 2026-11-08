package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// StandardLibraryTableOf is the table that holds entries, a finished run's standard-library packages by their
// standard-library keys (see StandardLibraryCaching's Collected), written for any machine: every file an entry's
// Withheld and export data name is renamed below goRoot to goRootMarker (see renameToGoRootMarker), and its findings
// are left out, since no run reads a finding of a package it restores from the table, which it never restores when
// Analyze was given it, and a finding names this machine's files. It refuses an entry that names a file outside
// goRoot, which no other machine could resolve, an entry in another format, and one that holds the walk of its files,
// which only a package Analyze was given has.
func StandardLibraryTableOf(entries map[cache.Key]StandardLibraryRunEntry, goRoot string) ([]byte, error) {
	rename := renameToGoRootMarker(goRoot)
	portable := make(map[cache.Key]cache.StandardLibraryEntry, len(entries))
	// In the keys' order, so the same entries refuse with the same error.
	for _, key := range slices.SortedFunc(maps.Keys(entries), func(left cache.Key, right cache.Key) int {
		return bytes.Compare(left[:], right[:])
	}) {
		collected := entries[key]
		var entry cacheEntry
		if decodeError := gob.NewDecoder(bytes.NewReader(collected.Entry)).Decode(&entry); decodeError != nil {
			return nil, fmt.Errorf("writing the standard library's table: decoding the entry of %s: %w",
				collected.PackagePath, decodeError)
		}
		if entry.Version != CacheFormatVersion {
			return nil, fmt.Errorf("writing the standard library's table: the entry of %s is in format %q, not %q",
				collected.PackagePath, entry.Version, CacheFormatVersion)
		}
		if len(entry.Files) > 0 {
			return nil, fmt.Errorf("writing the standard library's table: the entry of %s holds the walk of its "+
				"files, as only a package the run was asked to analyze does", collected.PackagePath)
		}
		for slot, stored := range entry.Analyzers {
			stored.Findings = nil
			entry.Analyzers[slot] = stored
		}
		withheld, renameError := renameWithheldFiles(entry.Withheld, rename)
		if renameError != nil {
			return nil, fmt.Errorf("writing the standard library's table: the entry of %s: %w", collected.PackagePath,
				renameError)
		}
		entry.Withheld = withheld
		var encoded bytes.Buffer
		if encodeError := gob.NewEncoder(&encoded).Encode(entry); encodeError != nil {
			return nil, fmt.Errorf("writing the standard library's table: encoding the entry of %s: %w",
				collected.PackagePath, encodeError)
		}
		var exportData []byte
		if collected.ExportData != nil {
			renamed, exportError := renameExportDataFiles(collected.ExportData, collected.PackagePath, rename)
			if exportError != nil {
				return nil, fmt.Errorf("writing the standard library's table: %w", exportError)
			}
			exportData = renamed
		}
		portable[key] = cache.StandardLibraryEntry{Entry: encoded.Bytes(), ExportData: exportData}
	}
	return cache.WriteStandardLibraryTable(portable), nil
}
