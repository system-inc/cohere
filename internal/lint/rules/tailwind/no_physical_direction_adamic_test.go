package tailwind

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// An Adamic `.a` file's class strings are checked as a `.ts` file's are (#kwt1htp).
func TestNoPhysicalDirectionChecksAnAdamicFileAsTypeScript(test *testing.T) {
	test.Parallel()
	source := "export const c = cn('flex ml-4');\n"
	rule_testing.ExpectSameFindings(test,
		rule_testing.Run(test, NoPhysicalDirection, "/repository/source/components/Thing.ts", source),
		rule_testing.Run(test, NoPhysicalDirection, "/repository/source/components/Thing.a", source))
}
