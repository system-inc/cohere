package our_tiers

import "testing"

// TestOwnsModuleReadsThePathAsOursOrNot: a module under the prefix is ours, and one whose path only begins with the
// same letters is not, so the prefix holds its trailing slash: `github.com/system-incorporated/x` names another
// organization (#4j6196c).
func TestOwnsModuleReadsThePathAsOursOrNot(test *testing.T) {
	test.Parallel()
	for modulePath, ours := range map[string]bool{
		"github.com/system-inc/cohere":       true,
		"github.com/system-inc/loom/command": true,
		"github.com/system-incorporated/x":   false,
		"github.com/system-inc":              false,
		"example.com/github.com/system-inc/": false,
		"":                                   false,
	} {
		if got := OwnsModule(modulePath); got != ours {
			test.Errorf("OwnsModule(%q) is %v, want %v", modulePath, got, ours)
		}
	}
}
