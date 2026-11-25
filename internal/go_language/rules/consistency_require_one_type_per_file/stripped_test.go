package consistency_require_one_type_per_file

import (
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// The types a file serves come from package served, which reads a type's declaration at a file's top level and a
// method's receiver in its signature, and stage 3's strip (#9fbrnra) leaves both typed. So with describe's body
// stripped from the type check, the stripped run finds what the plain one does, finding for finding: Second, the extra
// type. That body declares a local type with First's promoted label, which an index counting a body's types would
// count in the plain run and could not in the stripped one, where the type is never checked. It also takes a method
// value of Other, which other.go serves, and an index serving a type through a body's method values would serve
// Other in stripped.go in the plain run, from a selection the stripped run never records. marker's body is
// stripped too, and Variant stays a marker, since marker_type reads the emptiness of a body from the syntax, which
// keeps it. A strip that drops strings, which only describe uses, changes nothing, since served reads no import. A
// strip that hands the rule bodiless syntax makes marker look written in assembly, a real method, and Variant a second
// extra type.
func TestAStrippedBodyLeavesTheExtraTypeFinding(test *testing.T) {
	test.Parallel()
	functions := []string{"describe", "marker"}
	secondExtra := "`Second` is a second type with methods"
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions}, "stripped")
	// Not fatal, so a plain run that counts another type still shows the stripped run's difference below.
	if len(plainFindings) != 1 || !strings.Contains(plainFindings[0].Message, secondExtra) {
		test.Errorf("the plain run found %v, want Second alone reported as an extra type", plainFindings)
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
	added := parity.Difference(bodilessFindings, plainFindings)
	if len(bodilessFindings) != len(plainFindings)+1 || len(added) != 1 ||
		!strings.Contains(added[0].Message, "`Variant` is a second type with methods") {
		test.Errorf("a strip that hands the rule bodiless syntax found %v, want Variant reported beside %v",
			bodilessFindings, plainFindings)
	}
}
