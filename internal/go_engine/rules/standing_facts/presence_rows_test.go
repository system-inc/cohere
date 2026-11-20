package standing_facts_test

import (
	"cmp"
	"go/types"
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_presence"
)

// presenceUse is one object a presence test's host says the fixture's bodies use: its key, the identity the host
// names it by, and the bodies that use it, by their functions' names.
type presenceUse struct {
	key      fact_reach.Object
	identity build_reads.Identity
	users    []string
}

// presenceHost is a host's side of found in which every body stands but those of present, by their functions' names,
// and whose bodies use uses.
func presenceHost(test *testing.T, found fixture, present []string, uses ...presenceUse) *fakeHost {
	test.Helper()
	host := &fakeHost{standing: map[fact_reach.Place]bool{}, users: map[fact_reach.Object][]fact_reach.Place{}}
	for name, place := range found.places {
		host.places = append(host.places, place)
		host.standing[place] = !slices.Contains(present, name)
	}
	slices.SortFunc(host.places, func(first fact_reach.Place, second fact_reach.Place) int {
		return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Offset, second.Offset))
	})
	for _, use := range uses {
		host.used = append(host.used, fact_reach.UsedObject{Object: use.key, Identity: use.identity})
		for _, user := range use.users {
			host.users[use.key] = append(host.users[use.key], found.place(test, user))
		}
	}
	slices.SortFunc(host.used, func(first fact_reach.UsedObject, second fact_reach.UsedObject) int {
		return cmp.Compare(first.Object, second.Object)
	})
	return host
}

// requirePresence fails unless the bodies of leaves, by their functions' names, are exactly those that left the
// standing set for presence, each for its row, and those of facts the ones that left for a fact: every other body that
// stood stands, every body the settlement took out was taken out for its row's reason or its fact's, and the result
// counts by row the bodies of leaves.
func requirePresence(
	test *testing.T,
	found fixture,
	host *fakeHost,
	result *standing_facts.Result,
	leaves map[string]standing_presence.Row,
	facts map[string]fact_reach.Reason,
) {
	test.Helper()
	names := map[fact_reach.Place]string{}
	for name, place := range found.places {
		names[place] = name
		_, leaving := leaves[name]
		_, factLeaving := facts[name]
		if want := host.standing[place] && !leaving && !factLeaving; result.Stands(place.File, place.Offset) != want {
			test.Errorf("%s stands %t, want %t", name, !want, want)
		}
	}
	for _, removal := range result.Settlement.Removed {
		want := leaves[names[removal.Place]].Reason()
		if reason, isFact := facts[names[removal.Place]]; isFact {
			want = reason
		}
		if removal.Reason != want {
			test.Errorf("%s left for %q, want %q", names[removal.Place], removal.Reason, want)
		}
	}
	want := map[standing_presence.Row]int{}
	for _, row := range leaves {
		want[row]++
	}
	if !maps.Equal(result.PresenceRemoved, want) {
		test.Errorf("presence removed %v, want %v", result.PresenceRemoved, want)
	}
}

// fieldIdentity is how a host names the field named field of the fixture's struct type named typeName, by its
// objectpath, as the host's renderer names a field.
func (found fixture) fieldIdentity(test *testing.T, typeName string, field string) build_reads.Identity {
	test.Helper()
	declared, isTypeName := found.root.Types.Scope().Lookup(typeName).(*types.TypeName)
	if !isTypeName {
		test.Fatalf("the %s fixture declares no type named %s", found.root.ID, typeName)
	}
	structure, isStruct := declared.Type().Underlying().(*types.Struct)
	if !isStruct {
		test.Fatalf("%s is a %s, want a struct", typeName, declared.Type().Underlying())
	}
	for index := range structure.NumFields() {
		if structure.Field(index).Name() != field {
			continue
		}
		path, pathError := objectpath.For(structure.Field(index))
		if pathError != nil {
			test.Fatalf("%s.%s has no objectpath: %v", typeName, field, pathError)
		}
		identity := found.identity(typeName)
		identity.Path = path
		return identity
	}
	test.Fatalf("%s has no field %s", typeName, field)
	return build_reads.Identity{}
}

// scannererr's row (#xeafx79): Lines names Input, a scanner over the process's input, and Open names Assigned, which
// its own `=` makes a candidate, so both leave, and so does Remembered, which names Memory, whose reader is in memory,
// since the row names every *bufio.Scanner whatever made it. Sourced names Source, no scanner, and Count names
// nothing, so both stand.
func TestAStandingBodyNamingAScannerLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "scanning")
	host := presenceHost(test, found, nil,
		presenceUse{key: "scanning.Input", identity: found.identity("Input"), users: []string{"Lines"}},
		presenceUse{key: "scanning.Assigned", identity: found.identity("Assigned"), users: []string{"Open"}},
		presenceUse{key: "scanning.Memory", identity: found.identity("Memory"), users: []string{"Remembered"}},
		presenceUse{key: "scanning.Source", identity: found.identity("Source"), users: []string{"Sourced"}})
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"Lines": standing_presence.ScannerError, "Open": standing_presence.ScannerError,
		"Remembered": standing_presence.ScannerError}, nil)
}

// sqlrowserr's row (#xeafx79): Next names Rows, the first name a query's rows are assigned to, and leaves. Statement
// names Prepared, a prepared statement, and Count names nothing, so both stand.
func TestAStandingBodyNamingQueriedRowsLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "rowing")
	host := presenceHost(test, found, nil,
		presenceUse{key: "rowing.Rows", identity: found.identity("Rows"), users: []string{"Next"}},
		presenceUse{key: "rowing.Prepared", identity: found.identity("Prepared"), users: []string{"Statement"}})
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result,
		map[string]standing_presence.Row{"Next": standing_presence.SqlRowsError}, nil)
}

// screaming's row (#xeafx79): Reads compares with O_RDONLY, an underscored constant of a file importing syscall, and
// leaves. Plain compares with ReadOnly, which has no underscore, and Writes names nothing, so both stand.
func TestAStandingBodyNamingASystemConstantLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "screaming")
	host := presenceHost(test, found, nil,
		presenceUse{key: "screaming.O_RDONLY", identity: found.identity("O_RDONLY"), users: []string{"Reads"}},
		presenceUse{key: "screaming.ReadOnly", identity: found.identity("ReadOnly"), users: []string{"Plain"}})
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result,
		map[string]standing_presence.Row{"Reads": standing_presence.Screaming}, nil)
}

// emptyfunction's row (#xeafx79): shape is a marker, so Check, which selects its isShape, leaves, and so does Odd,
// which selects Other's isShape, a method of the marker's name on a receiver that does not implement shape. Firsts
// selects wide's first, and wide is no marker, so it stands, as Count, which selects nothing, does. Each method
// declared under the marker's name, isShape, leaves too: the rule's verdict on an empty one hangs on every selection.
func TestAStandingBodySelectingAMarkersMethodLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "marking")
	host := presenceHost(test, found, nil,
		presenceUse{key: "marking.shape.isShape", identity: found.method("shape", "isShape"), users: []string{"Check"}},
		presenceUse{key: "marking.Other.isShape", identity: found.method("Other", "isShape"), users: []string{"Odd"}},
		presenceUse{key: "marking.Pair.first", identity: found.method("Pair", "first"), users: []string{"Firsts"}})
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"Check": standing_presence.EmptyFunction, "Odd": standing_presence.EmptyFunction,
		"isShape": standing_presence.EmptyFunction}, nil)
}

// noany's bound (#xeafx79), one body taken out under each kind of key: Handle's parameter is a key, so Handle's own
// body leaves and Calls, which names it, leaves too; Record's Payload is a field key, so Reads, which selects it, and
// Builds, which builds Record, leave; Loose is a variable key, so Sets leaves; Getter's Get is an interface method
// key, so Gets, which selects Box's Get, leaves. Fills builds Rows, whose unkeyed element marks Inner's Note though it
// names only Rows, and leaves, and Touches names settings, a variable of an anonymous struct, and leaves. Anything
// keeps no declaration, so Holds, which names it, stands; so does Counts, which names Strict, a function holding no
// candidate, and so does Fresh, whose struct of settings' shape is written in place, with fields of its own.
func TestAStandingBodyTouchingANoAnyKeyLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "loose")
	host := presenceHost(test, found, nil,
		presenceUse{key: "loose.Handle", identity: found.identity("Handle"), users: []string{"Calls"}},
		presenceUse{key: "loose.Record", identity: found.identity("Record"), users: []string{"Builds", "Reads"}},
		presenceUse{key: "loose.Record.Payload", identity: found.fieldIdentity(test, "Record", "Payload"),
			users: []string{"Reads"}},
		presenceUse{key: "loose.Loose", identity: found.identity("Loose"), users: []string{"Sets"}},
		presenceUse{key: "loose.Box.Get", identity: found.method("Box", "Get"), users: []string{"Gets"}},
		presenceUse{key: "loose.Anything", identity: found.identity("Anything"), users: []string{"Holds"}},
		presenceUse{key: "loose.Strict", identity: found.identity("Strict"), users: []string{"Counts"}},
		presenceUse{key: "loose.Rows", identity: found.identity("Rows"), users: []string{"Fills"}},
		presenceUse{key: "loose.settings", identity: found.identity("settings"), users: []string{"Touches"}})
	host.functions = []fact_reach.Function{
		{Object: "loose.Handle", Identity: found.identity("Handle")},
		{Object: "loose.Strict", Identity: found.identity("Strict")},
	}
	host.declared = map[fact_reach.Place][]fact_reach.Object{
		found.place(test, "Handle"): {"loose.Handle"}, found.place(test, "Strict"): {"loose.Strict"},
	}
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"Handle": standing_presence.NoAny, "Calls": standing_presence.NoAny, "Builds": standing_presence.NoAny,
		"Reads": standing_presence.NoAny, "Sets": standing_presence.NoAny, "Gets": standing_presence.NoAny,
		"Fills": standing_presence.NoAny, "Touches": standing_presence.NoAny}, nil)
}

// The edge rows (#xeafx79), each direction. Start, TestRuns, Quiet, reached and reachedSub are present. By the callee
// direction, Start takes a test and starts helper with go, so helper leaves for testinggoroutine, and helper, present
// now, starts deeper, which leaves too; TestRuns hands subtest to Run by name, so subtest leaves for requireparallel.
// By the caller direction, the host's record says Late starts reached and TestLate hands reachedSub to Run, neither of
// which stands, so both callers leave. After the facts settle, Faulty, which leaves only for halt's no-return fact,
// is present, and afterFact, which it starts, leaves. Quiet takes no test, so quietHelper stands, and Standing, which
// stands, starts standingHelper, which stands too: a standing caller is stripped with its callee, and no rule reads
// either. RunsOutside hands outsideSub to Run in a file that is no test file, which requireparallel never reads, so
// outsideSub stands though RunsOutside is present.
func TestABodyAPresentCallerStartsLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadVariant(test, "edges", "edges [edges.test]")
	host := presenceHost(test, found, []string{"Start", "TestRuns", "Quiet", "reached", "reachedSub", "RunsOutside"},
		presenceUse{key: "edges.halt", identity: found.identity("halt"), users: []string{"Faulty"}})
	host.functions = []fact_reach.Function{{Object: "edges.halt", Identity: found.identity("halt")}}
	host.recordPresence(found)
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"helper": standing_presence.TestingGoroutine, "deeper": standing_presence.TestingGoroutine,
		"subtest": standing_presence.RequireParallel, "Late": standing_presence.TestingGoroutine,
		"TestLate": standing_presence.RequireParallel, "afterFact": standing_presence.TestingGoroutine,
	}, map[string]fact_reach.Reason{"Faulty": "no-return"})
}

// A body that writes a candidate of consistency-no-any's of its own, or declares a marker interface, leaves for the
// host's record of it (#xeafx79): Loose writes interface{} in a local variable's type, which no use records, and
// Literal writes any in a function literal's parameter, so both leave for noany; Sealed declares a marker of its own,
// so it leaves for emptyfunction, and so do Seals, which selects Box's method of the marker's name, and that method,
// seal, itself. Plain writes no candidate, and Opens selects a method of another name, so both stand.
func TestABodyHoldingAnyOrAMarkerOfItsOwnLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "owning")
	host := presenceHost(test, found, nil,
		presenceUse{key: "owning.Box.seal", identity: found.method("Box", "seal"), users: []string{"Seals"}},
		presenceUse{key: "owning.Box.open", identity: found.method("Box", "open"), users: []string{"Opens"}})
	host.recordPresence(found)
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"Loose": standing_presence.NoAny, "Literal": standing_presence.NoAny,
		"Sealed": standing_presence.EmptyFunction, "Seals": standing_presence.EmptyFunction,
		"seal": standing_presence.EmptyFunction,
	}, nil)
}

// noany's bound closed under the rule's flow (#xeafx79, the reader's M1): a body that names only a declaration the
// flow routes to a key leaves, as one naming the key does. Prints names x, defined from k; PrintsCached names cached,
// defined from values' result; Sorts names compare, defined from a literal whose parameters are keys; and Calls names
// g, whose parameter feeds kept, an edge only g's body records, so it is read once g leaves for naming kept. values'
// own body leaves as a key's function. Plain touches nothing the flow routes to a key, and stands.
func TestABodyTheFlowRoutesToANoAnyKeyLeavesForPresence(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "flowing")
	host := presenceHost(test, found, nil,
		presenceUse{key: "flowing.x", identity: found.identity("x"), users: []string{"Prints"}},
		presenceUse{key: "flowing.cached", identity: found.identity("cached"), users: []string{"PrintsCached"}},
		presenceUse{key: "flowing.compare", identity: found.identity("compare"), users: []string{"Sorts"}},
		presenceUse{key: "flowing.kept", identity: found.identity("kept"), users: []string{"g"}},
		presenceUse{key: "flowing.g", identity: found.identity("g"), users: []string{"Calls"}})
	for _, name := range []string{"Prints", "values", "PrintsCached", "Sorts", "g", "Calls", "Plain"} {
		host.functions = append(host.functions,
			fact_reach.Function{Object: fact_reach.Object("flowing." + name), Identity: found.identity(name)})
	}
	host.recordPresence(found)
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{
		"Prints": standing_presence.NoAny, "values": standing_presence.NoAny, "PrintsCached": standing_presence.NoAny,
		"Sorts": standing_presence.NoAny, "g": standing_presence.NoAny, "Calls": standing_presence.NoAny,
	}, nil)
}

// testinggoroutine judges only a package importing the standard library's testing (#xeafx79, the reader's S3): faking
// writes *testing.T of another package imported under that name, so Start, present, starts helper unheard, and helper
// stands.
func TestAGoStatementInAPackageNotImportingTestingTakesNothingOut(test *testing.T) {
	test.Parallel()
	found := loadFixture(test, "faking")
	host := presenceHost(test, found, []string{"Start"})
	host.recordPresence(found)
	result, _ := settle(test, found, host)
	requirePresence(test, found, host, result, map[string]standing_presence.Row{}, nil)
}
