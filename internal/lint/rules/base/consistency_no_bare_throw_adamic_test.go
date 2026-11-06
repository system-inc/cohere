package base

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// An Adamic `.a` file throws under the same rule as a `.ts` one, and `Service.test.a`, Adamic's test
// file, is exempt as `Service.test.ts` is (#kwt1htp).
func TestNoBareThrowReadsAnAdamicFileAsTypeScript(test *testing.T) {
	test.Parallel()
	source := "export function f(): void { throw new Error('x'); }"
	rule_testing.ExpectSameFindings(test,
		rule_testing.RunTyped(test, ConsistencyNoBareThrow, "/repository/source/modules/thing/Service.ts", source),
		rule_testing.RunTyped(test, ConsistencyNoBareThrow, "/repository/source/modules/thing/Service.a", source))
	rule_testing.ExpectClean(test,
		rule_testing.RunTyped(test, ConsistencyNoBareThrow, "/repository/source/modules/thing/Service.test.a", source))
}
