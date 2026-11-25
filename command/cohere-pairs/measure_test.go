package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Every Go child run of a pair carries its side's own `COHERE_SYSTEM_CACHE` (#syscache), whatever the pair's own
// environment named, so a pair controls what each side's system cache holds rather than inherit the person's.
func TestAGoChildRunCarriesItsSidesSystemCache(test *testing.T) {
	test.Parallel()
	output := test.TempDir()
	base := sideSystemCache(output, sideBase)
	environment := goChildEnvironment([]string{"COHERE_SYSTEM_CACHE=/the/person/s/own", "GOFLAGS=-tags=x",
		"HOME=/home"}, "/corpus", base)
	if !slices.Contains(environment, "COHERE_SYSTEM_CACHE="+base) ||
		slices.Contains(environment, "COHERE_SYSTEM_CACHE=/the/person/s/own") {
		test.Errorf("the run's environment is %q, want the base side's system cache %s alone", environment, base)
	}
	if sideSystemCache(output, sideBranch) == base {
		test.Error("both sides share one system cache")
	}
}

// A cache-on run starts its side's system cache empty: what an earlier round filled is moved aside into the trash,
// and a side with none yet moves nothing.
func TestACacheOnRunEmptiesItsSidesSystemCache(test *testing.T) {
	test.Parallel()
	output := test.TempDir()
	trash := filepath.Join(output, trashDirectoryName)
	if createError := os.MkdirAll(trash, 0o755); createError != nil {
		test.Fatal(createError)
	}
	systemCache := sideSystemCache(output, sideBranch)
	if moved, moveError := emptySystemCache(systemCache, trash, 0); moved != 0 || moveError != nil {
		test.Fatalf("a side with no system cache yet moved %d (%v), want none", moved, moveError)
	}
	table := filepath.Join(systemCache, "go", "standard-library", "ab", "abcdef")
	if createError := os.MkdirAll(filepath.Dir(table), 0o755); createError != nil {
		test.Fatal(createError)
	}
	if writeError := os.WriteFile(table, []byte("table"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if moved, moveError := emptySystemCache(systemCache, trash, 3); moved != 1 || moveError != nil {
		test.Fatalf("moved %d (%v), want the side's system cache moved aside", moved, moveError)
	}
	if _, statError := os.Lstat(systemCache); !os.IsNotExist(statError) {
		test.Errorf("the side's system cache is still there: %v", statError)
	}
	if _, statError := os.Stat(filepath.Join(trash, "00004-"+filepath.Base(systemCache), "go", "standard-library",
		"ab", "abcdef")); statError != nil {
		test.Errorf("want the table moved into the trash: %v", statError)
	}
}
