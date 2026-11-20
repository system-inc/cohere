package standing_facts_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// A standing body that names a variable hostport judges by its declaration leaves for presence (#xeafx79): Dial names
// Address, whose sole value is fmt.Sprintf("%s:%d", ...), so it leaves with hostport's reason. Port names nothing, and
// Counted names Count, whose format is "%d", so both stand. Remote names below.Address, another package's variable of
// Address's own name, which the build finds as below's and so never judges, and it stands too.
func TestAStandingBodyNamingAHostPortAddressLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "presence")
	dial, port := found.place(test, "Dial"), found.place(test, "Port")
	counted, remote := found.place(test, "Counted"), found.place(test, "Remote")
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{dial: true, port: true, counted: true, remote: true},
		places:   []fact_reach.Place{dial, port, counted, remote},
		used: []fact_reach.UsedObject{
			{Object: "below.Address", Identity: imported("below", "Address")},
			{Object: "presence.Address", Identity: found.identity("Address")},
			{Object: "presence.Count", Identity: found.identity("Count")},
		},
		users: map[fact_reach.Object][]fact_reach.Place{
			"below.Address": {remote}, "presence.Address": {dial}, "presence.Count": {counted},
		},
	}
	result, _ := settle(test, found, host)
	requireRemoved(test, result,
		fact_reach.Removal{Place: dial, Object: "presence.Address", Reason: standing_presence.HostPort.Reason()})
	for _, place := range []fact_reach.Place{port, counted, remote} {
		if !result.Stands(place.File, place.Offset) {
			test.Errorf("the body at %v does not stand, though it names no hostport candidate", place)
		}
	}
	if want := []fact_reach.Object{"presence.Address"}; !slices.Equal(host.usersAsked, want) {
		test.Errorf("named %q, want %q", host.usersAsked, want)
	}
	want := map[standing_presence.Row]int{standing_presence.HostPort: 1}
	if !maps.Equal(result.PresenceNamed, want) || !maps.Equal(result.PresenceRemoved, want) {
		test.Errorf("presence named %v and removed %v, want %v each", result.PresenceNamed, result.PresenceRemoved,
			want)
	}
}
