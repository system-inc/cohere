package host_port

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// A dial passed a package-level variable reports that variable's declaring Sprintf, so the finding at a package-level
// initializer hangs on the body that dials: here connect dials address, and the plain run reports address's format,
// naming connect's dial. With connect's body stripped from the type check, as stage 3 would strip it were it standing
// (#9fbrnra), the rule hears no dial, and the finding is lost. That dependence is why standing_presence's hostport row
// names address, so connect leaves what stands (standing_facts' TestAStandingBodyDialingAnAddressLeavesForPresence,
// over the same shape).
func TestAStrippedDialLosesTheAddressFinding(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"connect"}}, "stripped")
	reported := `address format "%s:%d" does not work with IPv6 (passed to net.Dial at L14)`
	if messages := parity.Messages(plainFindings); !slices.Equal(messages, []string{reported}) ||
		plainFindings[0].Line != 11 {
		test.Fatalf("the plain run found %v, want address's format reported at line 11: %s", plainFindings, reported)
	}
	if len(strippedFindings) != 0 {
		test.Errorf("stripping connect's body from the type check found %v, want nothing, since no dial is heard",
			strippedFindings)
	}
}
