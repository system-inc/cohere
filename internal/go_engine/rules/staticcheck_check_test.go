package rules

import (
	"fmt"
	"slices"
	"testing"

	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
)

// The registry's tables of staticcheck's checks are its suites, entry for entry and field for field, in order: a
// staticcheck release that adds, drops or reorders a check, or changes whether one is on by default or how its
// findings merge across builds, fails here by the check's name and the field, rather than leaving the registry
// registering a check the suite no longer has or missing one it gained. This is the one place outside the parity tests
// that links the suites, so the binary links none of their originals.
func TestTheStaticcheckTablesAreTheSuites(test *testing.T) {
	test.Parallel()
	drifts := slices.Concat(
		tableDrifts("staticcheckChecks", staticcheckChecks, slices.Concat(staticcheck.Analyzers, simple.Analyzers)),
		tableDrifts("stylecheckChecks", stylecheckChecks, stylecheck.Analyzers),
	)
	for _, drift := range drifts {
		test.Error(drift)
	}
}

// tableDrifts is each way table, named tableName, differs from suite: an entry differing from suite's at its index,
// by each field that differs, and an entry one holds past the other's end.
func tableDrifts(tableName string, table []staticcheckCheck, suite []*lint.Analyzer) []string {
	want := checksOfSuite(suite)
	drifts := []string{}
	for index := range max(len(table), len(want)) {
		switch {
		case index >= len(want):
			drifts = append(drifts, fmt.Sprintf("%s holds %s at %d, past the suite's %d checks", tableName,
				table[index].name, index, len(want)))
		case index >= len(table):
			drifts = append(drifts, fmt.Sprintf("%s ends at %d, before the suite's %s", tableName, len(table),
				want[index].name))
		case table[index].name != want[index].name:
			drifts = append(drifts, fmt.Sprintf("%s holds %s at %d, where the suite has %s", tableName,
				table[index].name, index, want[index].name))
		default:
			if table[index].nonDefault != want[index].nonDefault {
				drifts = append(drifts, fmt.Sprintf("%s says %s is off by default %v, where its documentation "+
					"says %v", tableName, want[index].name, table[index].nonDefault, want[index].nonDefault))
			}
			if table[index].mergeIfAll != want[index].mergeIfAll {
				drifts = append(drifts, fmt.Sprintf("%s says %s merges only when every build finds it %v, where "+
					"its documentation says %v", tableName, want[index].name, table[index].mergeIfAll,
					want[index].mergeIfAll))
			}
		}
	}
	return drifts
}

// checksOfSuite is suite as the registry's tables write it.
func checksOfSuite(suite []*lint.Analyzer) []staticcheckCheck {
	checks := []staticcheckCheck{}
	for _, wrapped := range suite {
		checks = append(checks, staticcheckCheck{
			name:       wrapped.Analyzer.Name,
			nonDefault: wrapped.Doc != nil && wrapped.Doc.NonDefault,
			mergeIfAll: wrapped.Doc != nil && wrapped.Doc.MergeIf == lint.MergeIfAll,
		})
	}
	return checks
}

// The comparison fails on a table one field off, by the check and the field, and on one with an entry missing: a
// planted table with SA1000 said off by default, and one with SA1000 left out, each fail, where the table itself
// passes, so a green TestTheStaticcheckTablesAreTheSuites is the comparison finding nothing to differ.
func TestTheTableComparisonFailsOnAPlantedDrift(test *testing.T) {
	test.Parallel()
	suite := slices.Concat(staticcheck.Analyzers, simple.Analyzers)
	if drifts := tableDrifts("staticcheckChecks", staticcheckChecks, suite); len(drifts) != 0 {
		test.Fatalf("the table itself drifts, so the plants below prove nothing: %q", drifts)
	}
	offByOneField := slices.Clone(staticcheckChecks)
	offByOneField[0].nonDefault = !offByOneField[0].nonDefault
	for _, planted := range []struct {
		name  string
		table []staticcheckCheck
		want  string
	}{
		{
			name:  "a field off",
			table: offByOneField,
			want:  "planted says SA1000 is off by default true, where its documentation says false",
		},
		{
			name:  "an entry missing",
			table: slices.Clone(staticcheckChecks[1:]),
			want:  "planted holds SA1001 at 0, where the suite has SA1000",
		},
	} {
		drifts := tableDrifts("planted", planted.table, suite)
		if !slices.Contains(drifts, planted.want) {
			test.Errorf("the table with %s drifts %q, where it was expected to drift %q", planted.name, drifts,
				planted.want)
		}
	}
}

// Every one of staticcheck's checks the registry registers has a port, since the registry links no original to run in
// a port's place, so portOf never panics as All runs.
func TestEveryRegisteredStaticcheckCheckHasAPort(test *testing.T) {
	test.Parallel()
	registered := registeredStaticcheckChecks()
	if len(registered) == 0 {
		test.Fatalf("the registry registers none of staticcheck's checks, so the check reads nothing")
	}
	for _, check := range registered {
		if _, held := ports[check.name]; !held {
			test.Errorf("the registry registers staticcheck's %s, and no port of it is in ports", check.name)
		}
	}
}
