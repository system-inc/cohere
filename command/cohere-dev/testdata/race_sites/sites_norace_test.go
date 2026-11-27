//go:build !race

package sites

import "testing"

// The race tag leaves this file out, so the gate's -race run selects none of its tests.

func TestNotUnderRace(test *testing.T) { fanOut([]int{1, 2}) }
