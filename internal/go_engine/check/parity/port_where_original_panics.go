package parity

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// panicked opens the error recovering gives a run of the original's that panicked, so a failure that opens with it is
// that panic and any other is a failure of the original's own.
const panicked = "the original panicked: "

// recovering is original with its run behind a recover, so a panic there is the action's error, opening with
// panicked, where the checker would let it end the process. Everything else of original's is kept.
func recovering(original *analysis.Analyzer) *analysis.Analyzer {
	wrapped := *original
	wrapped.Run = func(pass *analysis.Pass) (result any, runError error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				result, runError = nil, fmt.Errorf("%s%v", panicked, recovered)
			}
		}()
		return original.Run(pass)
	}
	return &wrapped
}

// RequirePortWhereOriginalPanics runs original, behind a recover, and port over the fixture package fixture under
// testdata's src, or the fixture module of that name (see fixtureLoad), where the original panics, so no comparison
// can hold it: the original must panic on every package of the fixture, each panic logged, and the port's findings
// there must be exactly the fixture's `// port finds:` records, matched by file, line and message, and column where
// one names it, as RequireOnlyAdjudicated matches them, each giving its reason as there, and their fixes exactly its
// `// port fixes:` records, held as there. An original that runs there, or fails other than by panicking, fails,
// since the fixture then belongs to RequireIdentical or RequireOnlyAdjudicated. A record of any other kind but one
// fails, since with no finding of the original's there is nothing for it to pair or rename, and so does a fixture that
// records nothing, a `// port ` comment the harness cannot read, and a package the port did not run on.
//
// The one other kind is `// port finds nothing`, for a fixture where the port is right to find nothing at all, its
// reason given as any record's is: the port must then find nothing there, each finding it makes failing, named whole,
// and the record beside a `finds:` record, which it contradicts, fails and compares nothing.
//
// Like RequireOnlyAdjudicated, it holds the port's fixes in a file with a fixed file beside it to that fixed file,
// byte for byte (see requireFixedFiles), reads the one package directory fixture names, and a file that cannot carry a
// comment, as one copied byte for byte cannot, takes its records beside the call, as given, each held as there, one
// with no Reason failing. It loads for the machine's own target, and Target.RequirePortWhereOriginalPanics for
// another.
func RequirePortWhereOriginalPanics(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	given ...Record,
) {
	test.Helper()
	requirePortWhereOriginalPanics(test, Target{}, original, port, testdata, fixture, given)
}

// requirePortWhereOriginalPanics is RequirePortWhereOriginalPanics with the fixture loaded for target.
func requirePortWhereOriginalPanics(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	given []Record,
) {
	test.Helper()
	portFindings := []Finding{}
	loads := loadFixtures(test, target, testdata, []string{fixture}, nil)
	for _, fixtures := range loads {
		outcomes, analyzeError := Analyze([]Pair{{Original: recovering(original), Port: port}}, fixtures.roots)
		if analyzeError != nil {
			test.Fatalf("%v", analyzeError)
		}
		outcome := outcomes[0]
		for _, failure := range outcome.Port.Failures {
			test.Fatalf("running the port on %s: %s", failure.Package, failure.Error)
		}
		// Every root has one action of the original's, and each that did not fail ran.
		if ran := len(fixtures.roots) - len(outcome.Original.Failures); ran > 0 {
			test.Errorf("the original no longer panics on %d of the %d packages of %s, so the fixture belongs to "+
				"RequireIdentical or RequireOnlyAdjudicated now", ran, len(fixtures.roots), fixture)
		}
		for _, failure := range outcome.Original.Failures {
			if !strings.HasPrefix(failure.Error, panicked) {
				test.Errorf("the original failed on %s other than by panicking: %s", failure.Package, failure.Error)
				continue
			}
			test.Logf("on %s, %s", failure.Package, failure.Error)
		}
		for _, finding := range outcome.Port.Findings {
			portFindings = append(portFindings, finding.WithFileNames(fixtures.fileName))
		}
	}
	folder := recordsFolder(test, loads[0])
	recorded := adjudicated(test, folder, loads[0].fileName, given)
	refusedAny := false
	for _, refused := range []struct {
		kind    string
		records []Finding
	}{
		{"renames", recorded.renamed},
		{"drops", recorded.dropped},
		{"moves", recorded.moved},
		{"withholds", recorded.withheld},
		{"omits", recorded.omitted},
	} {
		for _, record := range refused.records {
			test.Errorf("%s:%d records `%s:`, and where the original panics it finds nothing for the record to "+
				"cover: record what the port finds there with `finds:`", record.File, record.Line, refused.kind)
			refusedAny = true
		}
	}
	// A renaming adds its port's side to portAlone among the finds, so with one refused the finds cannot be told
	// from it, and nothing is compared.
	if refusedAny {
		return
	}
	// A record that the port finds nothing holds the whole fixture, so beside a record of what it finds it contradicts
	// that record, and nothing is compared.
	if len(recorded.findsNothing) > 0 {
		test.Logf("the fixture records that the port finds nothing, and the port made %d", len(portFindings))
		for _, record := range recorded.findsNothing {
			if len(recorded.portAlone) > 0 {
				test.Errorf("%s:%d records that the port finds nothing, beside %d records of what it finds",
					record.File, record.Line, len(recorded.portAlone))
			}
		}
		if len(recorded.portAlone) > 0 {
			return
		}
		for _, found := range portFindings {
			test.Errorf("the port found %s, where the fixture records that it finds nothing", found)
		}
		return
	}
	test.Logf("the fixture records %d findings, the port made %d", len(recorded.portAlone), len(portFindings))
	if len(recorded.portAlone) == 0 {
		test.Fatalf("the fixture %s records nothing, so the comparison proves nothing", fixture)
	}
	portAlone := asRecorded(portFindings)
	for _, unrecorded := range Difference(portAlone, recorded.portAlone) {
		test.Errorf("the port found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range Difference(recorded.portAlone, portAlone) {
		test.Errorf("the fixture records that the port finds %s, and it does not", unmatched)
	}
	requirePlaced(test, "the port finds", recorded.placed, portFindings)
	requireRecordedFixes(test, recorded, portFindings, editLines(loads))
	requireFixedFiles(test, folder, loads[0].fileName, fixture, portFindings)
}
