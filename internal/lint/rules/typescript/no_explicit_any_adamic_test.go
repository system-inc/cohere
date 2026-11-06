package typescript

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// An Adamic `.a` file is TypeScript source, so `any` in one reports as it does in `.ts` (#kwt1htp).
func TestNoExplicitAnyReportsAnAdamicFileAsTypeScript(test *testing.T) {
	test.Parallel()
	source := "export const value: any = 1;\n"
	rule_testing.ExpectSameFindings(test,
		rule_testing.RunTyped(test, NoExplicitAny, "/repository/source/Thing.ts", source),
		rule_testing.RunTyped(test, NoExplicitAny, "/repository/source/Thing.a", source))
}
