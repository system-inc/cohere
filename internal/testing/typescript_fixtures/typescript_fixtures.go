// Package typescript_fixtures is every TypeScript fixture this repository holds, read by the directories that
// hold them, for a test whose corpus is our TypeScript: the format proofs that the fix pass's guard's tree is
// the tree the formatter would parse (#dk2502g).
//
// Those proofs once walked the whole module for .ts files, so Go's test cache keyed them on every directory in
// it, and their packages reran on every landing for a change to any file (#0zdyk51). Reading the fixture
// directories by name keys them on exactly the files they judge. guard's
// TestEveryTypeScriptFixtureIsUnderANamedRoot walks the module, as a whole-tree check does, so a fixture placed
// anywhere else fails there by name rather than leaving the corpus without it.
package typescript_fixtures

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/repository"
)

// Roots are the directories, relative to the repository's root and slash-separated, that hold every TypeScript
// fixture in the repository.
var Roots = []string{
	"internal/lint/ecmascript/canonical_function_hash/testdata",
	"internal/lint/ecmascript/high_level_intermediate_representation/testdata",
	"internal/lint/rules/adamic/testdata",
	"internal/lint/rules/nexus/testdata",
	"internal/lint/rules/react/conformance/testdata",
	"internal/lint/rules/tailwind/collapse/testdata",
	"internal/rename/testdata",
	"internal/text_rules/conformance/testdata",
	"internal/unused_exports/testdata",
}

// Files is every file under Roots whose extension, lowercased, is one of extensions. A root that is missing
// fails the test, since a corpus that lost a root would still compare and pass on the rest.
func Files(test testing.TB, extensions ...string) []string {
	test.Helper()
	var files []string
	for _, root := range Roots {
		files = append(files, Under(repository.Path(test, root), extensions...)...)
	}
	return files
}

// Under is every file under root whose extension, lowercased, is one of extensions, outside dependency, build
// and cache directories and the TypeScript checkout, never following a symbolic link. It reads whatever root
// holds, so a test that calls it on the repository's root is keyed on every file there.
func Under(root string, extensions ...string) []string {
	wanted := map[string]bool{}
	for _, extension := range extensions {
		wanted[extension] = true
	}
	skipped := map[string]bool{
		"node_modules": true,
		".git":         true,
		".cache":       true,
		".next":        true,
		"dist":         true,
		"TypeScript":   true,
	}
	var files []string
	// Ignored: the walk skips every path it cannot read and returns only nil or SkipDir, so WalkDir has no error
	// left to report. A root that is missing holds no files, which Files catches before it gets here.
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipped[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if wanted[strings.ToLower(filepath.Ext(path))] && entry.Type()&fs.ModeSymlink == 0 {
			files = append(files, path)
		}
		return nil
	})
	return files
}
