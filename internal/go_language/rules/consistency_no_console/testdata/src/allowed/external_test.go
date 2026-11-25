package allowed_test

import (
	"fmt"
	"testing"
)

// An external test package's file is a test's too.
func TestPrints(test *testing.T) {
	test.Parallel()
	fmt.Print("external\n")
}
