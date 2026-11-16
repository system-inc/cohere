package check

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// The save gives no alias to an entry encoded without judging whether its findings name another package's file: it is
// refused as a problem of the session, never read as independent, while an entry judged independent beside it, with
// its second key known, is aliased.
func TestTheSaveRefusesAnUnjudgedEntryAnAlias(test *testing.T) {
	test.Parallel()

	unjudged := &packages.Package{ID: "example.com/unjudged"}
	independent := &packages.Package{ID: "example.com/independent"}
	// Neither imports anything, so each one's second key is known from its own digest alone.
	session := &CacheSession{
		own: map[*packages.Package]cache.Key{unjudged: cache.DigestOf([]byte("unjudged own")),
			independent: cache.DigestOf([]byte("independent own"))},
		keys: map[*packages.Package]cache.Key{unjudged: cache.DigestOf([]byte("unjudged key")),
			independent: cache.DigestOf([]byte("independent key"))},
	}
	encoded := map[*packages.Package]encodedEntry{
		unjudged:    {},
		independent: {positionDependent: judgedPosition(false)},
	}
	aliases := map[cache.Key]cache.Key{}
	session.aliasWritten([]*packages.Package{unjudged, independent}, encoded, aliases)

	unjudgedSecond, unjudgedKnown := session.secondKey(unjudged)
	independentSecond, independentKnown := session.secondKey(independent)
	if !unjudgedKnown || !independentKnown {
		test.Fatalf("the second keys were known %t and %t, want both", unjudgedKnown, independentKnown)
	}
	if _, aliased := aliases[unjudgedSecond]; aliased {
		test.Errorf("the unjudged entry of %s was given an alias, want none", unjudged.ID)
	}
	if aliases[independentSecond] != session.keys[independent] {
		test.Errorf("the independent entry of %s was not aliased to its key, want it", independent.ID)
	}
	if len(session.counts.Problems) != 1 || !strings.Contains(session.counts.Problems[0], unjudged.ID) {
		test.Errorf("the session noted %q, want one problem naming %s", session.counts.Problems, unjudged.ID)
	}
}
