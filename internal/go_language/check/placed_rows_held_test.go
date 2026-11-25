package check

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// A restore holds no placed answer decoded where nothing reads one (#ycnvyvd): a warm run's hit, and the host's shared
// decode of an entry it kept for the builds, read the entry's placed answers only to refuse one whose columns do not
// read, and hold none, though the entry holds them. Held, they were about a third of a warm run's live heap over
// check's closure, every one dead weight.
func TestARestoreHoldsNoPlacedAnswerNothingReads(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	placedModule(test, directory, "func Use() int {\n\treturn bottom.Double(2)\n}\n")
	cold := runWithSession(test, directory, caching, rules)
	entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, "example.com/placed/bottom"))
	if placed, placedError := entry.Placed.placed(); !stored || placedError != nil || len(placed) == 0 {
		test.Fatalf("want bottom's entry to hold placed answers, so the restore has some to drop, got %d (%v)",
			len(placed), placedError)
	}
	warm := runWithSession(test, directory, caching, rules)
	if len(warm.session.hits) == 0 {
		test.Fatal("the warm run restored nothing, so no hit was read")
	}
	for hit, restored := range warm.session.hits {
		if len(restored.placed) > 0 {
			test.Errorf("%s: a warm hit holds %d placed answers decoded, want none", hit.ID, len(restored.placed))
		}
	}
	host, _ := hostSessionWith(test, directory, testCaching(test, rules), rules, nil)
	if len(host.Host.entries) == 0 {
		test.Fatal("the host kept no entry for the builds, so no shared decode was read")
	}
	for identifier, kept := range host.Host.entries {
		shared, decodeError := kept.decoded()
		if decodeError != nil {
			test.Fatalf("%s: decoding the host's kept entry: %v", identifier, decodeError)
		}
		if len(shared.placed) > 0 {
			test.Errorf("%s: the host's shared decode holds %d placed answers decoded, want none", identifier,
				len(shared.placed))
		}
	}
}
