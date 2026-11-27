package sites

import "testing"

// Linux alone compiles this file, so the gate's darwin -race run selects none of its tests, though one starts a
// goroutine of its own.

func TestOnLinux(test *testing.T) { fanOut([]int{1, 2}) }

func TestStartsOnLinux(test *testing.T) { go func() {}() }
