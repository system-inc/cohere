//go:build unix

// The stand-in engine here is a /bin/sh script, so the file builds where /bin/sh is.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/lint/rules/nexus"
	"github.com/system-inc/cohere/internal/release/dispatch"
)

// TestSwiftVocabularyIsWrittenOnceAndReused holds the cached path: the embedded bytes, in the project's
// cache, under their hash, and not written again while that file exists.
func TestSwiftVocabularyIsWrittenOnceAndReused(t *testing.T) {
	root := t.TempDir()

	path, remove, err := swiftVocabularyFile(root, false)
	if err != nil {
		t.Fatal(err)
	}
	remove()
	if filepath.Dir(path) != cacheDirectory(root) {
		t.Fatalf("written to %s, outside the project's cache %s", path, cacheDirectory(root))
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the vocabulary was not left for the next run: %v", err)
	}
	if !bytes.Equal(written, nexus.AbbreviationsFile()) {
		t.Fatal("the file is not the vocabulary cohere embeds")
	}

	// Backdated, so a rewrite would be seen in the time even if it wrote the same bytes.
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	again, _, err := swiftVocabularyFile(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("the second run used %s, and the first wrote %s", again, path)
	}
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(past) {
		t.Fatalf("an existing vocabulary file was written again (%v)", err)
	}
}

// TestSwiftVocabularyLeavesAnotherCoheresAlone is two cohere builds with different words on one root, which
// happens daily. The other one may have returned its file to an engine that has not opened it yet, so
// writing this one's must not remove it.
func TestSwiftVocabularyLeavesAnotherCoheresAlone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(cacheDirectory(root), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(cacheDirectory(root), "abbreviations-000000000000.json")
	if err := os.WriteFile(other, []byte(`{"abbreviations":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	path, _, err := swiftVocabularyFile(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if path == other {
		t.Fatal("another cohere's file was taken as this vocabulary's")
	}
	if !isRegularFile(path) {
		t.Fatal("this vocabulary was not written")
	}
	if !isRegularFile(other) {
		t.Fatal("another cohere's vocabulary was removed while it could still be in use")
	}
}

// TestSwiftVocabularyUnderNoCacheWritesNothingToTheProject holds what `--no-cache` promises: nothing under
// the project's `.cache`. The engine still gets the words, from a file that is gone after the run.
func TestSwiftVocabularyUnderNoCacheWritesNothingToTheProject(t *testing.T) {
	root := t.TempDir()

	path, remove, err := swiftVocabularyFile(root, true)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(written, nexus.AbbreviationsFile()) {
		t.Fatalf("the engine would not have read the vocabulary from %s (%v)", path, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(err) {
		t.Fatalf("--no-cache wrote into the project's .cache (%v)", err)
	}
	remove()
	if isRegularFile(path) {
		t.Fatalf("the temporary vocabulary %s outlived the run", path)
	}
}

// TestSwiftRunHandsTheEngineTheVocabulary proves the front door passes the file, which the tests above
// cannot: they drive swiftVocabularyFile directly and would pass with the call deleted from
// runSwiftEngine. The stand-in engine copies whatever `--abbreviations` names while it runs, and the
// copy must be the vocabulary cohere embeds.
func TestSwiftRunHandsTheEngineTheVocabulary(t *testing.T) {
	root := t.TempDir()
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if err != nil {
		t.Fatal(err)
	}
	received := filepath.Join(t.TempDir(), "received.json")
	engine := fakeSwiftEngine(t, `while [ $# -gt 0 ]; do
	if [ "$1" = --abbreviations ]; then cp "$2" '`+received+`'; fi
	shift
done
cat '`+filepath.Join(fixtures, "Clean.jsonl")+`'`)
	t.Setenv(dispatch.SwiftEngineOverrideVariable, engine)

	if _, err := runSwiftEngine(projectLocation{Root: root}, map[string]bool{}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(received)
	if err != nil {
		t.Fatalf("the engine was not passed --abbreviations: %v", err)
	}
	if !bytes.Equal(got, nexus.AbbreviationsFile()) {
		t.Fatal("the engine was handed a file that is not the vocabulary cohere embeds")
	}
}
