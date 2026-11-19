package standing_facts_test

import "github.com/system-inc/cohere/internal/go_engine/fact_reach"

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
	users      map[fact_reach.Object][]fact_reach.Place
	recorded   map[fact_reach.Place][]fact_reach.Question
	declared   map[fact_reach.Place][]fact_reach.Object
	kinds      []string

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

func (host *fakeHost) Standing() (map[fact_reach.Place]bool, bool) {
	host.count("Standing")
	return host.standing, !host.whole
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

func (host *fakeHost) Kinds() []string {
	host.count("Kinds")
	return host.kinds
}
