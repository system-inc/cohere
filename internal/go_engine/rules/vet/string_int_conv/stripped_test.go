package string_int_conv

import (
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The fix formats the number with fmt.Sprint, importing fmt only where the file does not already (addImport), which
// it reads from the file's imports and scope, so stage 3's strip (#9fbrnra) changes nothing only if it keeps an import
// that only a stripped body uses. With greet's body stripped, the only user of fmt, the stripped run finds what the
// plain one does, finding for finding and fix for fix: digits' conversion, its fix writing fmt.Sprint under the import
// the file holds. A strip that drops the import loses fmt from the file's scope, and the fix imports fmt a second time.
// The fix reads the scope of the file it is in, so the fixture stands on the strip recording that scope by the original
// file the rule reads, not the copy it type-checked: without that, addImport's Innermost panics on a nil scope.
func TestAnImportOnlyAStrippedBodyUsesStillServesTheFix(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"greet"}}, "stripped")
	if len(plainFindings) != 1 || len(plainFindings[0].Fixes) == 0 ||
		plainFindings[0].Fixes[0].Message != "Format the number as a decimal" ||
		len(plainFindings[0].Fixes[0].Edits) != 1 || plainFindings[0].Fixes[0].Edits[0].NewText != "fmt.Sprint" {
		test.Fatalf("the plain run found %v, want digits' conversion, its fix writing fmt.Sprint and no import",
			plainFindings)
	}
	if !reflect.DeepEqual(strippedFindings, plainFindings) {
		test.Errorf("stripping greet's body from the type check found %v, where the plain run found %v",
			strippedFindings, plainFindings)
	}

	_, droppingFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"greet"}, DroppingImports: true}, "stripped")
	if len(droppingFindings) != 1 || len(droppingFindings[0].Fixes) == 0 ||
		len(droppingFindings[0].Fixes[0].Edits) == 0 ||
		!strings.Contains(droppingFindings[0].Fixes[0].Edits[0].NewText, `import "fmt"`) {
		test.Errorf("a strip that drops fmt's import found %v, want the fix importing fmt again", droppingFindings)
	}
}
