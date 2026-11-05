package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cacheTableFiles lists a project's cache table files, one per section and per recorded run, sorted.
func cacheTableFiles(test *testing.T, root string) []string {
	test.Helper()
	files, err := filepath.Glob(filepath.Join(cacheDirectory(root), "*.gob"))
	if err != nil {
		test.Fatal(err)
	}
	return files
}

// removeCacheTable removes a project's cache table, file by file, so the next run starts without it. A
// project with no table to remove fails here, since a control that removed nothing proves nothing.
func removeCacheTable(test *testing.T, root string) {
	test.Helper()
	files := cacheTableFiles(test, root)
	if len(files) == 0 {
		test.Fatalf("no cache table in %s to remove", cacheDirectory(root))
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			test.Fatal(err)
		}
	}
}
