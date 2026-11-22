package consistency_require_matching_file_name

import (
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The type a file is judged against comes from package served, which reads a type's declaration at a file's top level
// and a method's receiver in its signature, and stage 3's strip (#9fbrnra) leaves both typed. So with describe's body
// stripped from the type check, the stripped run finds what the plain one does, finding for finding: example.go
// serves First alone and is not named for it. That body declares a local type with First's promoted label, which an
// index counting a body's types would count in the plain run, so the file would serve two types and go unjudged, and
// could not in the stripped one. marker's body is stripped too, and Variant stays a marker, since marker_type reads the
// emptiness of a body from the syntax, which keeps it. A strip that drops strings, which only describe uses, changes
// nothing, since served reads no import. A strip that hands the rule bodiless syntax makes marker look written in
// assembly, a real method, so the file serves Variant too and is not judged.
func TestAStrippedBodyLeavesTheFileNameFinding(test *testing.T) {
	test.Parallel()
	functions := []string{"describe", "marker"}
	firstServed := "`First` is the one type with methods served in `example.go`"
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions}, "stripped")
	// Not fatal, so a plain run that counts another type still shows the stripped run's difference below.
	if len(plainFindings) != 1 || !strings.Contains(plainFindings[0].Message, firstServed) {
		test.Errorf("the plain run found %v, want example.go reported for First", plainFindings)
	}
	if !reflect.DeepEqual(strippedFindings, plainFindings) {
		test.Errorf("stripping describe and marker's bodies from the type check found %v, where the plain run found %v",
			strippedFindings, plainFindings)
	}

	_, droppingFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions, DroppingImports: true}, "stripped")
	if !reflect.DeepEqual(droppingFindings, plainFindings) {
		test.Errorf("a strip that drops strings' import found %v, where the plain run found %v",
			droppingFindings, plainFindings)
	}

	_, bodilessFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions, BodilessSyntax: true}, "stripped")
	if len(bodilessFindings) != 0 {
		test.Errorf("a strip that hands the rule bodiless syntax found %v, want nothing on a file serving two types",
			bodilessFindings)
	}
}
