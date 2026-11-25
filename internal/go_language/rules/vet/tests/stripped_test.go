package tests

import (
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// An example's name is looked up in the package's imports (lookupObjects), so an import that only a stripped body
// uses must stay in the type check for stage 3's strip (#9fbrnra) to change nothing. With upper's body stripped, the
// only user of strings, the stripped run finds what the plain one does, finding for finding: ExampleBuilder names a
// type strings declares, and ExampleReplacer_Missing a method Replacer lacks. A strip that drops the import loses
// strings from the package's imports, and both examples name an unknown identifier.
func TestAnImportOnlyAStrippedBodyUsesStillServesAnExample(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"upper"}}, "stripped")
	missingMethod := "ExampleReplacer_Missing refers to unknown field or method: Replacer.Missing"
	if messages := parity.Messages(plainFindings); !slices.Equal(messages, []string{missingMethod}) {
		test.Fatalf("the plain run found %q, want ExampleReplacer_Missing's missing method alone", messages)
	}
	if !reflect.DeepEqual(strippedFindings, plainFindings) {
		test.Errorf("stripping upper's body from the type check found %v, where the plain run found %v",
			strippedFindings, plainFindings)
	}

	_, droppingFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"upper"}, DroppingImports: true}, "stripped")
	// In the order findings sort, by how each prints, line 12 before line 9.
	unknown := []string{
		"ExampleReplacer_Missing refers to unknown identifier: Replacer",
		"ExampleBuilder refers to unknown identifier: Builder",
	}
	if messages := parity.Messages(droppingFindings); !slices.Equal(messages, unknown) {
		test.Errorf("a strip that drops strings' import found %q, want %q", messages, unknown)
	}
}
