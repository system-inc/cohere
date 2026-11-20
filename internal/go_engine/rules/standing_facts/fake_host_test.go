package standing_facts_test

import (
	"go/types"
	"maps"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// fakeHost is a host's side of the facts package as a test writes it, counting each call the build's pass makes of
// it. whole is a package the build lints whole, whose Standing reports false.
type fakeHost struct {
	whole      bool
	standing   map[fact_reach.Place]bool
	places     []fact_reach.Place
	functions  []fact_reach.Function
	imported   []fact_reach.Question
	asking     map[string]fact_reach.Asking
	orderBound []fact_reach.Named
	used       []fact_reach.UsedObject
	presence   map[fact_reach.Place]fact_reach.PresenceRecord
	markers    []string
	users      map[fact_reach.Object][]fact_reach.Place
	recorded   map[fact_reach.Place][]fact_reach.Question
	declared   map[fact_reach.Place][]fact_reach.Object
	kinds      []fact_reach.Kind
	parameters map[fact_reach.Object]int

	// calls counts the pass's calls, by method name, and Asking's by `Asking ` and the key too. usersAsked is each
	// object the pass asked the users of, in order, which are the objects it named.
	calls      map[string]int
	usersAsked []fact_reach.Object
}

func (host *fakeHost) count(call string) {
	if host.calls == nil {
		host.calls = map[string]int{}
	}
	host.calls[call]++
}

// Standing is a copy of standing at each call, as a host's is, since the pass takes bodies out of it.
func (host *fakeHost) Standing() (map[fact_reach.Place]bool, bool) {
	host.count("Standing")
	return maps.Clone(host.standing), !host.whole
}

func (host *fakeHost) Places() []fact_reach.Place {
	host.count("Places")
	return host.places
}

func (host *fakeHost) Functions() []fact_reach.Function {
	host.count("Functions")
	return host.functions
}

func (host *fakeHost) Imported() []fact_reach.Question {
	host.count("Imported")
	return host.imported
}

func (host *fakeHost) Asking(key string) (fact_reach.Asking, bool) {
	host.count("Asking")
	host.count("Asking " + key)
	asking, known := host.asking[key]
	return asking, known
}

func (host *fakeHost) OrderBound() []fact_reach.Named {
	host.count("OrderBound")
	return host.orderBound
}

func (host *fakeHost) Used() []fact_reach.UsedObject {
	host.count("Used")
	return host.used
}

func (host *fakeHost) Presence(place fact_reach.Place) fact_reach.PresenceRecord {
	host.count("Presence")
	return host.presence[place]
}

func (host *fakeHost) Markers() []string {
	host.count("Markers")
	return host.markers
}

func (host *fakeHost) Users(object fact_reach.Object) []fact_reach.Place {
	host.count("Users")
	host.usersAsked = append(host.usersAsked, object)
	return host.users[object]
}

func (host *fakeHost) Recorded(place fact_reach.Place) []fact_reach.Question {
	host.count("Recorded")
	return host.recorded[place]
}

func (host *fakeHost) Declared(place fact_reach.Place) []fact_reach.Object {
	host.count("Declared")
	return host.declared[place]
}

func (host *fakeHost) Kinds() []fact_reach.Kind {
	host.count("Kinds")
	return host.kinds
}

func (host *fakeHost) Parameters(key fact_reach.Object) int {
	host.count("Parameters")
	return host.parameters[key]
}

// recordPresence holds the presence record the host's own run would make of each body of found
// (standing_presence.ReadBodies), each function's key the fixture's package path and its name, and declares each body's
// function at its place under that key.
func (host *fakeHost) recordPresence(found fixture) {
	key := func(name string) fact_reach.Object { return fact_reach.Object(found.root.PkgPath + "." + name) }
	keys := func(functions []*types.Func) []fact_reach.Object {
		var started []fact_reach.Object
		for _, function := range functions {
			started = append(started, key(function.Name()))
		}
		return started
	}
	host.presence = map[fact_reach.Place]fact_reach.PresenceRecord{}
	read := standing_presence.ReadBodies(found.root.Fset, found.root.Syntax, found.root.TypesInfo, found.root.Types)
	for place, body := range read {
		host.presence[place] = fact_reach.PresenceRecord{GoStarts: keys(body.GoStarts),
			RunStarts: keys(body.RunStarts), OwnAny: body.OwnAny, LocalMarkers: body.LocalMarkers}
	}
	if host.declared == nil {
		host.declared = map[fact_reach.Place][]fact_reach.Object{}
	}
	for name, place := range found.places {
		host.declared[place] = []fact_reach.Object{key(name)}
	}
}
