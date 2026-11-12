package check

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sessionRun is one load and one run with a cache session of their own, as the engine makes them.
type sessionRun struct {
	loaded  *load.Loaded
	result  *Result
	session *CacheSession
}

// runWithSession loads directory with a new session over caching and runs rules over what it loaded.
func runWithSession(test *testing.T, directory string, caching *Caching, rules []rule.Rule) sessionRun {
	test.Helper()
	session := NewCacheSession(caching, rules)
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	return sessionRun{loaded: loaded, result: result, session: session}
}

// sessionKey is the key session gave the package at path.
func sessionKey(test *testing.T, session *CacheSession, path string) cache.Key {
	test.Helper()
	for keyed, key := range session.keys {
		if keyed.PkgPath == path && keyed.ForTest == "" {
			return key
		}
	}
	test.Fatalf("the session keyed no package %s", path)
	return cache.Key{}
}

// A package the cache holds is never parsed. Warm, nothing is parsed or read from export data, since nothing missed
// needs a type, and the Result is the cold run's, walk counts included, though no restored package has syntax. With
// user's entry corrupt, only user is parsed, base's types come from its export data, and marked's finding in user,
// which only base's restored fact gives, still fires.
func TestAHitIsNeverParsedAndAMissTypeChecksAgainstItsExportData(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	counts := &runCounts{}
	rules := []rule.Rule{houseRule(markedAnalyzer), houseRule(targetAnalyzer), countedRule("countedExport", counts)}
	caching := testCaching(test, rules)
	cold := runWithSession(test, root, caching, rules)
	if cold.loaded.FilesParsed != 2 || cold.result.Cache.Written != 2 {
		test.Fatalf("want the cold run to parse both files and write both packages, got %d parsed and %+v",
			cold.loaded.FilesParsed, cold.result.Cache)
	}

	warm := runWithSession(test, root, caching, rules)
	if warm.loaded.FilesParsed != 0 || warm.loaded.PackagesFromExportData != 0 || warm.result.Cache.Hit != 2 ||
		warm.session.exportDataReads.Load() != 0 {
		test.Fatalf("want nothing parsed or read warm, got %d parsed, %d read from %d stored, %+v",
			warm.loaded.FilesParsed, warm.loaded.PackagesFromExportData, warm.session.exportDataReads.Load(),
			warm.result.Cache)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(warm.result), withoutTimesOrCache(cold.result)) {
		test.Fatalf("warm\n%+v\ncold\n%+v", withoutTimesOrCache(warm.result), withoutTimesOrCache(cold.result))
	}

	userKey := sessionKey(test, warm.session, "example.com/fixture/user")
	if putError := caching.Store.Put(userKey, []byte("corrupt")); putError != nil {
		test.Fatalf("corrupting user's entry: %v", putError)
	}
	edited := runWithSession(test, root, caching, rules)
	if edited.loaded.FilesParsed != 1 || edited.loaded.PackagesFromExportData != 1 ||
		edited.session.exportDataReads.Load() != 1 {
		test.Fatalf("want user alone parsed and base alone read from export data, got %d parsed and %d read from %d "+
			"stored", edited.loaded.FilesParsed, edited.loaded.PackagesFromExportData,
			edited.session.exportDataReads.Load())
	}
	if !reflect.DeepEqual(withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result)) {
		test.Fatalf("edited\n%+v\ncold\n%+v", withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result))
	}
	marked := 0
	for _, found := range edited.result.Findings {
		if found.Rule == rule.Namespace+"test-marked" && found.Message == "calls Marked, which base marked" {
			marked++
		}
	}
	if marked != 1 {
		test.Fatalf("want user's call to Marked reported from base's restored fact, got %+v", edited.result.Findings)
	}
}

// A stored fact whose object base's export data does not hold is skipped and counted, never an error, and the
// findings are the cold run's.
func TestAStoredFactOnAnObjectTheExportDataLacksIsSkipped(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	rules := []rule.Rule{houseRule(markedAnalyzer)}
	caching := testCaching(test, rules)
	cold := runWithSession(test, root, caching, rules)
	baseKey := sessionKey(test, cold.session, "example.com/fixture/base")
	slot := rule.Namespace + "test-marked"
	rewriteStoredEntry(test, caching.Store, baseKey, func(entry *cacheEntry) {
		planted := entry.Analyzers[slot]
		if len(planted.ObjectFacts) != 1 {
			test.Fatalf("want base's one fact on Marked stored, got %+v", planted.ObjectFacts)
		}
		gone := planted.ObjectFacts[0]
		gone.Path = "NoSuchObject"
		planted.ObjectFacts = append(planted.ObjectFacts, gone)
		entry.Analyzers[slot] = planted
	})
	userKey := sessionKey(test, cold.session, "example.com/fixture/user")
	if putError := caching.Store.Put(userKey, []byte("corrupt")); putError != nil {
		test.Fatalf("corrupting user's entry: %v", putError)
	}

	edited := runWithSession(test, root, caching, rules)
	if edited.result.Cache.FactsSkipped != 1 {
		test.Fatalf("want the planted fact skipped and counted, got %+v", edited.result.Cache)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result)) {
		test.Fatalf("edited\n%+v\ncold\n%+v", withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result))
	}
}

// A package with a load diagnostic is never saved, nor one that imports it, which is ill-typed, while a package
// beside them with none is.
func TestAPackageWithALoadDiagnosticIsNeverSaved(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod": "module example.com/broken\n\ngo 1.24\n",
		"broken/broken.go": "package broken\n\n// Broken returns a string as an int.\n" +
			"func Broken() int { return \"no\" }\n",
		"importer/importer.go": "package importer\n\nimport \"example.com/broken/broken\"\n\nvar _ = broken.Broken\n",
		"fine/fine.go":         "package fine\n\n// Fine is well typed.\nfunc Fine() int { return 1 }\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	rules := []rule.Rule{houseRule(quietAnalyzer)}
	caching := testCaching(test, rules)
	ran := runWithSession(test, root, caching, rules)
	if len(ran.loaded.Diagnostics) == 0 {
		test.Fatalf("want the planted type error among the load's diagnostics")
	}
	for path, saved := range map[string]bool{
		"example.com/broken/broken":   false,
		"example.com/broken/importer": false,
		"example.com/broken/fine":     true,
	} {
		if _, stored := caching.Store.Get(sessionKey(test, ran.session, path)); stored != saved {
			test.Errorf("want %s saved %t, got %t", path, saved, stored)
		}
	}
	again := runWithSession(test, root, caching, rules)
	var parsed []*packages.Package
	for _, analyzedPackage := range again.loaded.Packages {
		if len(analyzedPackage.Syntax) > 0 {
			parsed = append(parsed, analyzedPackage)
		}
	}
	if len(parsed) != 2 || !reflect.DeepEqual(again.loaded.Diagnostics, ran.loaded.Diagnostics) {
		test.Fatalf("want broken and its importer checked again, with the same diagnostics, got %d parsed and %+v",
			len(parsed), again.loaded.Diagnostics)
	}
}

// unimportedFiles are a module whose package base declares Marked and has an in-package test, so the load analyzes
// "base [base.test]" and loads plain base as a root it never analyzes, which nothing imports.
var unimportedFiles = map[string]string{
	"go.mod": "module example.com/unimported\n\ngo 1.24\n",
	"base/base.go": "package base\n\n// Marked is the function markedAnalyzer exports a fact for.\n" +
		"func Marked() int { return 1 }\n",
	"base/base_test.go": "package base\n\nimport \"testing\"\n\n" +
		"// TestMarked calls Marked.\nfunc TestMarked(test *testing.T) {\n\ttest.Parallel()\n\t_ = Marked()\n}\n",
}

// userFile is the root added in the second run, which imports plain base and calls Marked, so marked reports the
// call only through the fact base exports.
const userFile = "package user\n\nimport \"example.com/unimported/base\"\n\n" +
	"// Use calls Marked from outside the package that marked it.\nfunc Use() int { return base.Marked() }\n"

// writeFiles writes files into root.
func writeFiles(test *testing.T, root string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
}

// A package the load reaches that nothing analyzes or imports is stored without facts, since no fact analyzer ran
// on it. Once a new root imports it, its facts are asked for, so it must miss: it is checked and analyzed again, the
// root's finding that depends on its fact fires, and the Result is a run's without the cache over the same tree.
func TestAPackageStoredWithoutFactsMissesOnceARootImportsIt(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, unimportedFiles)
	rules := []rule.Rule{houseRule(markedAnalyzer)}
	caching := testCaching(test, rules)
	first := runWithSession(test, root, caching, rules)
	plainBase := func(session *CacheSession) *packages.Package {
		for keyed := range session.keys {
			if keyed.ID == "example.com/unimported/base" {
				return keyed
			}
		}
		test.Fatalf("the load reached no plain base")
		return nil
	}
	restored, decodeError := decodedStoredEntry(test, caching.Store, first.session.known, plainBase(first.session),
		first.session.keys[plainBase(first.session)])
	if decodeError != nil || covers(restored, first.session.known.facts) {
		test.Fatalf("want plain base stored without facts, got %v", decodeError)
	}

	writeFiles(test, root, map[string]string{"user/user.go": userFile})
	second := runWithSession(test, root, caching, rules)
	if second.session.restored(plainBase(second.session)) {
		test.Errorf("want plain base missed once user imports it, got a hit")
	}
	uncachedLoad, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading %s again: %v", root, loadError)
	}
	uncached, runError := Run(uncachedLoad.Packages, rules, Options{})
	if runError != nil {
		test.Fatalf("running without the cache: %v", runError)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(second.result), withoutTimesOrCache(uncached)) {
		test.Fatalf("cached\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(second.result),
			withoutTimesOrCache(uncached))
	}
	fired := false
	for _, found := range second.result.Findings {
		fired = fired || found.Message == "calls Marked, which base marked"
	}
	if !fired {
		test.Fatalf("want user's call to Marked reported from base's fact, got %+v", second.result.Findings)
	}
}

// An entry whose export data is gone from under it, as when files are removed from the store by hand, is read the long
// way and never an error: with user's entry corrupt so user needs base's types, base's export data, kept by its digest
// apart from base's entry (#5s1x2hg), does not read, so the load checks base from source too, and the result is the
// cold run's, with no problem said.
func TestAnEntryWhoseExportDataIsGoneHasItsPackageCheckedFromSource(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	rules := []rule.Rule{houseRule(markedAnalyzer), houseRule(targetAnalyzer)}
	directory := test.TempDir()
	caching := cachingAt(directory, rules)
	cold := runWithSession(test, root, caching, rules)

	digest := storedRecordAt(test, caching.Store, sessionKey(test, cold.session, "example.com/fixture/base")).ExportData
	if digest == nil {
		test.Fatal("want base's record to name its export data")
	}
	blob := exportDataKey(digest).String()
	if removeError := os.Remove(
		filepath.Join(directory, ".cache", "cohere", "go", blob[:2], blob),
	); removeError != nil {
		test.Fatalf("removing base's export data: %v", removeError)
	}
	userKey := sessionKey(test, cold.session, "example.com/fixture/user")
	if putError := caching.Store.Put(userKey, []byte("corrupt")); putError != nil {
		test.Fatalf("corrupting user's entry: %v", putError)
	}

	edited := runWithSession(test, root, caching, rules)
	if edited.loaded.FilesParsed != 2 || edited.loaded.PackagesFromExportData != 0 {
		test.Fatalf("want base and user both checked from source, got %d parsed and %d read from export data",
			edited.loaded.FilesParsed, edited.loaded.PackagesFromExportData)
	}
	if len(edited.result.Cache.Problems) != 0 {
		test.Fatalf("want no problem said, got %v", edited.result.Cache.Problems)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result)) {
		test.Fatalf("edited\n%+v\ncold\n%+v", withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result))
	}
}

// A whole entry of format 11, as a key held one before records (#fq7dxvx), is a miss under a key of this format, never
// read: were a store of 11's entries ever under these keys, its packages would be checked from source and their
// entries written again, with the cold run's result.
func TestAnEntryOfTheFormatBeforeRecordsIsAMiss(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	rules := []rule.Rule{houseRule(markedAnalyzer), houseRule(targetAnalyzer)}
	caching := testCaching(test, rules)
	cold := runWithSession(test, root, caching, rules)
	userKey := sessionKey(test, cold.session, "example.com/fixture/user")
	entry, _, stored := storedEntryAt(caching.Store, userKey)
	if !stored {
		test.Fatal("the cold run wrote no entry of user")
	}
	entry.Version = "go-engine-cache-11"
	var older bytes.Buffer
	if encodeError := gob.NewEncoder(&older).Encode(entry); encodeError != nil {
		test.Fatal(encodeError)
	}
	if putError := caching.Store.Put(userKey, older.Bytes()); putError != nil {
		test.Fatal(putError)
	}

	edited := runWithSession(test, root, caching, rules)
	if edited.result.Cache.Hit != 1 || edited.loaded.FilesParsed != 1 {
		test.Fatalf("want user a miss checked from source and base a hit, got %d parsed and %+v",
			edited.loaded.FilesParsed, edited.result.Cache)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result)) {
		test.Fatalf("edited\n%+v\ncold\n%+v", withoutTimesOrCache(edited.result), withoutTimesOrCache(cold.result))
	}
}
