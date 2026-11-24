package go_engine

import (
	"bytes"
	"encoding/gob"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storedBody is what this test reads of a file of the findings store: the format and the body an entry's record holds
// (check's entryRecord, whose fields gob matches by name), nothing for any other file.
type storedBody struct {
	Version string
	Inline  []byte
}

// bodiesIn is the body of every entry's record in root's findings store that holds one, by the file's path under the
// store, which is its key's. A file that is no record, a listing, a verdict, an alias table or export data, has no
// body, and is passed over.
func bodiesIn(test *testing.T, root string) map[string][]byte {
	test.Helper()
	bodies := map[string][]byte{}
	walkError := filepath.WalkDir(storeDirectory(root), func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		// The store writes each file's sha256 before its bytes (cache.Store's Get).
		const sumSize = 32
		if len(contents) < sumSize {
			return nil
		}
		var record storedBody
		// Ignored: a file gob refuses is no entry's record.
		if gob.NewDecoder(bytes.NewReader(contents[sumSize:])).Decode(&record) != nil {
			return nil
		}
		if !strings.HasPrefix(record.Version, "go-engine-cache-") || record.Inline == nil {
			return nil
		}
		relative, relativeError := filepath.Rel(storeDirectory(root), path)
		if relativeError != nil {
			return relativeError
		}
		bodies[relative] = record.Inline
		return nil
	})
	if walkError != nil {
		test.Fatalf("reading the store of %s: %v", root, walkError)
	}
	return bodies
}

// Two runs over one tree, each starting from an empty findings store, write every entry's body with the same bytes
// under the same name (#g9kaytv): a body is named by the digest of its bytes wherever another build's record points at
// it, so a body whose bytes moved between runs of unchanged source reads as not held, and its package is checked from
// source for nothing. Neither run reads a system cache, so the store holds the standard library's entries too, whose
// analyses hold the facts.
func TestTwoRunsOverOneTreeWriteTheSameBodies(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	if cold := runCachedEngine(test, root, "--no-system-cache"); cold.counts.Written == 0 {
		test.Fatalf("want the first run to write the store, got %+v", cold.counts)
	}
	first := bodiesIn(test, root)
	// The first run's store is moved aside whole, so the second starts from an empty one, as the first did.
	if renameError := os.Rename(storeDirectory(root), filepath.Join(test.TempDir(), "first")); renameError != nil {
		test.Fatal(renameError)
	}
	if cold := runCachedEngine(test, root, "--no-system-cache"); cold.counts.Hit != 0 || cold.counts.Written == 0 {
		test.Fatalf("want the second run to hit nothing and write the store, got %+v", cold.counts)
	}
	second := bodiesIn(test, root)
	if len(first) == 0 || len(first) != len(second) {
		test.Fatalf("want both runs to write the same bodies, at least one, got %d then %d", len(first), len(second))
	}
	differing := 0
	for name, body := range first {
		again, held := second[name]
		if !held {
			test.Errorf("the second run wrote no body under %s", name)
			continue
		}
		if !bytes.Equal(body, again) {
			differing++
		}
	}
	if differing > 0 {
		test.Errorf("%d of %d bodies differ in their bytes between two runs over one tree", differing, len(first))
	}
}
