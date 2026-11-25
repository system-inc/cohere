package check

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// placedCorruptions are every way placed refuses an entry's placed answers, each made of columns that read back: a
// column shorter than the rest, each one in turn, a table with a digest more than it has files, a row naming a kind or
// a file outside its table, either side, and an empty answer.
var placedCorruptions = map[string]func(stored *storedPlaced){
	"the kind indexes short":   func(stored *storedPlaced) { stored.KindIndexes = stored.KindIndexes[1:] },
	"the file indexes short":   func(stored *storedPlaced) { stored.FileIndexes = stored.FileIndexes[1:] },
	"the offsets short":        func(stored *storedPlaced) { stored.OffsetSteps = stored.OffsetSteps[1:] },
	"the parameters short":     func(stored *storedPlaced) { stored.Parameters = stored.Parameters[1:] },
	"the values short":         func(stored *storedPlaced) { stored.Values = stored.Values[1:] },
	"a digest more than files": func(stored *storedPlaced) { stored.FileDigests = append(stored.FileDigests, nil) },
	"a kind past the table":    func(stored *storedPlaced) { stored.KindIndexes[0] = len(stored.Kinds) },
	"a kind before the table":  func(stored *storedPlaced) { stored.KindIndexes[0] = -1 },
	"a file past the table":    func(stored *storedPlaced) { stored.FileIndexes[0] = len(stored.Files) },
	"a file before the table":  func(stored *storedPlaced) { stored.FileIndexes[0] = -1 },
	"an empty answer":          func(stored *storedPlaced) { stored.Values[0] = nil },
}

// placedReadable are answers whose columns read back: two files, one with no digest, two kinds, a function's own
// answer and a parameter's, and two rows in one file, so an offset steps from the one before it.
func placedReadable() storedPlaced {
	return storedPlacedOf([]fact_resolver.PlacedAnswer{
		{Kind: "function_purity", File: "/module/a.go", FileDigest: []byte("a"), Offset: 40, Parameter: -1,
			Encoded: []byte("true")},
		{Kind: "function_purity", File: "/module/a.go", FileDigest: []byte("a"), Offset: 120, Parameter: -1,
			Encoded: []byte("false")},
		{Kind: "odd_slice_argument", File: "/module/b.go", Offset: 16, Parameter: 0, Encoded: []byte("true")},
	})
}

// readError refuses exactly what placed refuses, with the same words, and accepts exactly what it reads back, so a hit
// that holds no answers is a miss for every column a decode holding them would refuse (#59560yx).
func TestReadErrorRefusesExactlyWhatPlacedRefuses(test *testing.T) {
	test.Parallel()
	fixtures := map[string]storedPlaced{"answers that read": placedReadable(), "no answers": storedPlacedOf(nil),
		"no columns": {}}
	for name, corrupt := range placedCorruptions {
		broken := placedReadable()
		corrupt(&broken)
		fixtures[name] = broken
	}
	for name, stored := range fixtures {
		readError := stored.readError()
		_, placedError := stored.placed()
		_, refused := placedCorruptions[name]
		if (readError != nil) != refused || (placedError != nil) != refused {
			test.Errorf("%s: readError says %v and placed %v, want both refused %t", name, readError, placedError,
				refused)
			continue
		}
		if refused && readError.Error() != placedError.Error() {
			test.Errorf("%s: readError says %q and placed %q, want the same words", name, readError, placedError)
		}
	}
}

// A hit's decode, which holds no placed answers, still refuses every column placed refuses (#59560yx): bottom's entry,
// written cold, has its placed answers broken each way placed refuses, and the warm run misses it rather than restore
// it. Unbroken, it is a hit, so a miss is the column's doing.
func TestAHitIsAMissForEveryColumnPlacedRefuses(test *testing.T) {
	test.Parallel()
	cases := map[string]func(stored *storedPlaced){"unbroken": nil}
	for name, corrupt := range placedCorruptions {
		cases[name] = corrupt
	}
	for name, corrupt := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			rules := []rule.Rule{houseRule(placedProbe)}
			caching := testCaching(test, rules)
			placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
			cold := runWithSession(test, directory, caching, rules)
			key := sessionKey(test, cold.session, "example.com/placed/bottom")
			rewriteStoredEntry(test, caching.Store, key, func(entry *cacheEntry) {
				if len(entry.Placed.KindIndexes) < 2 {
					test.Fatalf("want bottom's entry to hold placed answers to break, got %+v", entry.Placed)
				}
				if corrupt != nil {
					corrupt(&entry.Placed)
				}
			})
			warm := runWithSession(test, directory, caching, rules)
			hit := false
			for restored := range warm.session.hits {
				hit = hit || restored.PkgPath == "example.com/placed/bottom"
			}
			if hit != (corrupt == nil) {
				test.Errorf("bottom restored from its entry %t, want %t", hit, corrupt == nil)
			}
		})
	}
}
