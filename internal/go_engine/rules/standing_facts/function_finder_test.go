package standing_facts_test

import (
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Each way a host names a function finds the build's own, or finds none and names the object: a function found holds
// the host's no-return fact here, so only those not found are named, in the host's order.
func TestTheBuildsFunctionIsFoundByEachKindOfIdentity(test *testing.T) {
	test.Parallel()
	found := loadFacts(test)
	caller := found.place(test, "Caller")
	// The one method of Probe's interface is a function no name reaches, only an objectpath, which the build's own
	// types give here as the host's renderer gave it there.
	probe, isVariable := found.root.Types.Scope().Lookup("Probe").(*types.Var)
	if !isVariable {
		test.Fatalf("the facts fixture declares no variable named Probe")
	}
	probed, isInterface := probe.Type().(*types.Interface)
	if !isInterface || probed.NumMethods() != 1 {
		test.Fatalf("Probe is a %s, want an interface of one method", probe.Type())
	}
	pingPath, pathError := objectpath.For(probed.Method(0))
	if pathError != nil {
		test.Fatalf("Probe's method has no objectpath: %v", pathError)
	}
	byPath := func(path objectpath.Path) build_reads.Identity {
		identity := found.identity("Probe")
		identity.Path = path
		return identity
	}
	ofKind := func(kind build_reads.IdentityKind) build_reads.Identity {
		identity := found.identity("stop")
		identity.Kind = kind
		return identity
	}
	host := &fakeHost{
		standing: map[fact_reach.Place]bool{caller: true},
		places:   []fact_reach.Place{caller},
		functions: []fact_reach.Function{
			{Object: "a method of a named type", Identity: found.method("gauge", "halt"), NoReturn: true},
			{Object: "a method the type does not declare", Identity: found.method("gauge", "missing")},
			{Object: "a method of a name that is no type", Identity: found.method("stop", "halt")},
			{Object: "a function by its objectpath", Identity: byPath(pingPath)},
			{Object: "an objectpath that reaches nothing", Identity: byPath(pingPath + "7")},
			{Object: "a name that is no function", Identity: found.identity("Probe")},
			{Object: "a name the package does not declare", Identity: found.identity("missing")},
			{Object: "a function two imports down", Identity: imported("deep", "Pure")},
			{Object: "a package this one does not import", Identity: imported("elsewhere", "Pure")},
			{Object: "an import's identity", Identity: ofKind(build_reads.IdentityImport)},
			{Object: "an unknown identity", Identity: ofKind(build_reads.IdentityUnknown)},
		},
	}
	result, _ := settle(test, found, host)
	wantNamed := []fact_reach.Object{
		"a method the type does not declare",
		"a method of a name that is no type",
		"an objectpath that reaches nothing",
		"a name that is no function",
		"a name the package does not declare",
		"a package this one does not import",
		"an import's identity",
		"an unknown identity",
	}
	if !slices.Equal(host.usersAsked, wantNamed) {
		test.Errorf("named %q, want %q", host.usersAsked, wantNamed)
	}
	if result.FunctionsCompared != len(host.functions) || result.FunctionsNamed != len(wantNamed) {
		test.Errorf("compared %d functions and named %d, want %d and %d", result.FunctionsCompared,
			result.FunctionsNamed, len(host.functions), len(wantNamed))
	}
}
