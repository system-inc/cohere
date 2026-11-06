// An external test package is a package of its own, all test files, so its one finding is at its first file, and it
// is judged by the folder it sits in, though its import path ends in formatoptions_test.
package formatoptions_test // want `^Folder name "formatoptions" runs its words together\. Use "format_options" instead\.`

import (
	"testing"

	"flagged/formatoptions"
)

func TestTabs(test *testing.T) {
	test.Parallel()
	if formatoptions.Tabs {
		test.Fatal("Tabs changed")
	}
}
