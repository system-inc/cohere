package check

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// A session restores a second load when the long-line wrap wrote a file. When that restore cannot compute the keys,
// here because a file the first load listed is gone, the run saves nothing and counts nothing, and says why, rather
// than saving entries under the first load's keys, for bytes no longer on disk.
func TestARestoreWhoseKeysFailLeavesNoKeysOfTheLoadBefore(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod":       "module example.com/twice\n\ngo 1.24\n",
		"base/base.go": "package base\n\n// Value is one.\nfunc Value() int { return 1 }\n",
		"user/user.go": "package user\n\nimport \"example.com/twice/base\"\n\n// Twice is two.\n" +
			"func Twice() int { return base.Value() * 2 }\n",
	})
	rules := []rule.Rule{structtagRule}
	session := NewCacheSession(testCaching(test, rules), rules)
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		test.Fatalf("loading %s: %v", root, loadError)
	}
	if len(session.keys) == 0 {
		test.Fatal("the first restore keyed nothing, so the second could not leave its keys behind")
	}
	if removeError := os.Remove(filepath.Join(root, "base", "base.go")); removeError != nil {
		test.Fatal(removeError)
	}
	var reachable []*packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
	session.Restore(load.LoaderSource, reachable, loaded.Packages)
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	if result.Cache.Written != 0 || result.Cache.Hit != 0 || result.Cache.Missed != 0 {
		test.Fatalf("want nothing saved or counted after keys that failed, got %+v", result.Cache)
	}
	if len(result.Cache.Problems) == 0 {
		test.Fatal("want the keys' failure among the problems")
	}
}

// A restore reads the store's entries many at once, and makes the same hits, stored under the same keys and decoded
// the same, as reading them one at a time does: whole entries under their own keys, one reached through an alias, and
// a corrupt one, which is a miss. Under the race detector it also shows the workers write nothing of the session's.
func TestARestoreReadsTheSameHitsAsOneAtATime(test *testing.T) {
	test.Parallel()

	const importers = 40
	files := map[string]string{
		"go.mod":       "module example.com/many\n\ngo 1.24\n",
		"base/base.go": "package base\n\n// Marked is marked.\nfunc Marked() int { return 1 }\n",
	}
	for index := range importers {
		name := fmt.Sprintf("p%02d", index)
		files[name+"/"+name+".go"] = "package " + name + "\n\nimport \"example.com/many/base\"\n\n" +
			"// Use calls Marked.\nfunc Use() int { return base.Marked() }\n"
	}
	root := test.TempDir()
	writeFiles(test, root, files)
	rules := []rule.Rule{houseRule(markedAnalyzer), packageClauseRule}
	directory := test.TempDir()
	caching := cachingAt(directory, rules)
	runCached(test, loadAt(test, root).Packages, caching, rules)

	warm := loadAt(test, root)
	keyContext := caching.Context
	keyContext.Build = append(append([]string{}, keyContext.Build...), "loader="+load.LoaderSource)
	keyed, keysError := cache.Keys(warm.Packages, keyContext, nil, cache.FileDigests{}, nil)
	if keysError != nil {
		test.Fatalf("computing the keys: %v", keysError)
	}
	byPath := map[string]*packages.Package{}
	for keyedPackage := range keyed.Keys {
		byPath[keyedPackage.PkgPath] = keyedPackage
	}
	corrupt, aliased := byPath["example.com/many/p05"], byPath["example.com/many/p09"]
	if corrupt == nil || aliased == nil {
		test.Fatalf("the load reached no p05 or no p09: %v", byPath)
	}
	// p09's entry moves to a key of its own, which an alias under its key names, and its own key holds no entry, as
	// when a second key restored it. Its export data stays where its entry names it, by digest.
	moved := cache.Key(sha256.Sum256([]byte("moved p09")))
	aliasedEntry, entryStored := caching.Store.Get(keyed.Keys[aliased])
	if !entryStored {
		test.Fatal("the cold run wrote no entry of p09")
	}
	for key, contents := range map[cache.Key][]byte{
		moved:                         aliasedEntry,
		aliasKey(keyed.Keys[aliased]): moved[:],
		keyed.Keys[corrupt]:           []byte("corrupt"),
	} {
		if putError := caching.Store.Put(key, contents); putError != nil {
			test.Fatal(putError)
		}
	}
	aliasedName := keyed.Keys[aliased].String()
	if removeError := os.Remove(filepath.Join(directory, ".cache", "cohere", "go", aliasedName[:2],
		aliasedName)); removeError != nil {
		test.Fatal(removeError)
	}

	session := NewCacheSession(caching, rules)
	var reachable []*packages.Package
	packages.Visit(warm.Packages, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
	session.Restore(load.LoaderSource, reachable, warm.Packages)
	if len(session.hits) != importers || session.restored(corrupt) || session.stored[aliased] != moved {
		test.Fatalf("want %d hits, p05 a miss and p09 stored at its alias's key, got %d hits, p05 hit %t, p09 at %s",
			importers, len(session.hits), session.restored(corrupt), session.stored[aliased])
	}
	for _, restoring := range reachable {
		key, hasKey := session.keys[restoring]
		if !hasKey {
			continue
		}
		one := session.readHit(restoring, key)
		restored, hit := session.hits[restoring]
		if hit != (one.restored != nil) || session.stored[restoring] != one.storedKey {
			test.Fatalf("%s: want hit %t at %s as one read alone, got hit %t at %s", restoring.PkgPath,
				one.restored != nil, one.storedKey, hit, session.stored[restoring])
		}
		if hit && !reflect.DeepEqual(comparableRestored(restored), comparableRestored(one.restored)) {
			test.Fatalf("%s: want what one read alone decoded\n%+v\ngot\n%+v", restoring.PkgPath,
				comparableRestored(one.restored), comparableRestored(restored))
		}
	}
}

// loadAt is a load of root's packages of the test's own.
func loadAt(test *testing.T, root string) *load.Loaded {
	test.Helper()
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", root, loadError)
	}
	return loaded
}

// comparableRestored is a restored package but for its export data reader, a function, which reflect.DeepEqual
// equates only when both are nil.
func comparableRestored(restored *restoredPackage) restoredPackage {
	compared := *restored
	types := *restored.types
	types.ReadExportData = nil
	compared.types = &types
	return compared
}
