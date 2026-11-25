package consistency_require_separated_folder_words

import (
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// The rule reads the import path, the file names and the package clause, and no type at all, so nothing stage 3's
// strip (#9fbrnra) takes from the type check reaches it. With trimmed's body stripped, the stripped run finds what the
// plain one does, finding for finding: the goengine folder runs its words together. So do a strip that drops strings,
// which only trimmed uses, and one that hands the rule bodiless syntax, since neither touches what the rule reads.
func TestAStrippedBodyLeavesTheFolderFinding(test *testing.T) {
	test.Parallel()
	functions := []string{"trimmed"}
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions}, "stripped/goengine")
	// Not fatal, so a plain run that finds something else still shows the stripped run's difference below.
	if len(plainFindings) != 1 ||
		!strings.HasPrefix(plainFindings[0].Message, `Folder name "goengine" runs its words together.`) {
		test.Errorf("the plain run found %v, want the goengine folder reported", plainFindings)
	}
	if !reflect.DeepEqual(strippedFindings, plainFindings) {
		test.Errorf("stripping trimmed's body from the type check found %v, where the plain run found %v",
			strippedFindings, plainFindings)
	}

	_, droppingFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions, DroppingImports: true}, "stripped/goengine")
	if !reflect.DeepEqual(droppingFindings, plainFindings) {
		test.Errorf("a strip that drops strings' import found %v, where the plain run found %v",
			droppingFindings, plainFindings)
	}

	_, bodilessFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: functions, BodilessSyntax: true}, "stripped/goengine")
	if !reflect.DeepEqual(bodilessFindings, plainFindings) {
		test.Errorf("a strip that hands the rule bodiless syntax found %v, where the plain run found %v",
			bodilessFindings, plainFindings)
	}
}
