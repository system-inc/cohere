package no_shadow_probe

import (
	"testing"

	"github.com/system-inc/cohere/internal/rule_testing"
)

// The typed harness caches built programs on disk, and that cache outlives every individual test, so
// it is swept when this binary exits rather than by any one test.
func TestMain(m *testing.M) {
	rule_testing.RunTestsAndCleanUp(m)
}
