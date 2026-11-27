package sites

import "testing"

// Go ignores a file whose name starts with an underscore, so no run selects its tests.

func TestIgnored(test *testing.T) { fanOut([]int{1, 2}) }
