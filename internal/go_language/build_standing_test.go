package go_language

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/rules/standing_facts"
)

// A build's standing bodies answer nothing for a package whose standing_facts result settled nothing: no result, one of
// another type, one that lints the package whole, and one whose settlement left nothing standing. A settled result is
// kept for its package, and the build's judgment reads it there alone, by the file a `//line` directive names at the
// offset as parsed, as the facts' reach keys a body: the parsed file's name at the same offset does not stand, and
// neither does another package's body at the same place.
func TestABuildsStandingBodiesReadEachPackagesSettledResult(test *testing.T) {
	test.Parallel()

	directed := fact_reach.Place{File: "/project/directed.rl", Offset: 40}
	for _, each := range []struct {
		name  string
		after any
	}{
		{"no result", nil},
		{"a result of another type", "settled"},
		{"a package linted whole", &standing_facts.Result{}},
		{"a settlement with nothing standing", &standing_facts.Result{Partial: true,
			Settlement: &fact_reach.Settlement{Standing: map[fact_reach.Place]bool{}}}},
	} {
		standing := &buildStanding{settled: map[string]*standing_facts.Result{}}
		if answered := standing.bodiesOf("example.com/p", each.after); answered != nil ||
			len(standing.settled) != 0 {
			test.Errorf("%s: the build answered %v and kept %v, want nothing", each.name, answered,
				standing.settled)
		}
	}

	standing := &buildStanding{settled: map[string]*standing_facts.Result{}}
	settled := &standing_facts.Result{Partial: true,
		Settlement: &fact_reach.Settlement{Standing: map[fact_reach.Place]bool{directed: true}}}
	// The build's host kept nothing here, so the take answers nil, but the package's result is kept all the same.
	standing.bodiesOf("example.com/p", settled)
	if standing.settled["example.com/p"] != settled {
		test.Fatalf("the build kept %v, want the package's settled result", standing.settled)
	}
	for _, each := range []struct {
		packageIdentifier string
		filename          string
		offset            int
		stands            bool
	}{
		{"example.com/p", directed.File, directed.Offset, true},
		{"example.com/p", "/project/planted.go", directed.Offset, false},
		{"example.com/p", directed.File, directed.Offset + 1, false},
		{"example.com/q", directed.File, directed.Offset, false},
	} {
		if stands := standing.stands(each.packageIdentifier, each.filename, each.offset); stands != each.stands {
			test.Errorf("%s's %s at %d stands %t, want %t", each.packageIdentifier, each.filename, each.offset,
				stands, each.stands)
		}
	}
	if bodies, findings := (*buildStanding)(nil).counts(); bodies != 0 || findings != 0 {
		test.Errorf("a build with no standing bodies counted %d bodies and %d findings, want none", bodies, findings)
	}
}
