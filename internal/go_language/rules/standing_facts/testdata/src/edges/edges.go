// Package edges is the build's package the edge rows' presence test settles: each function says who starts it.
package edges

import "testing"

// Start takes a test and starts helper with go, so testinggoroutine reads helper's body.
func Start(t *testing.T) { go helper(t) }

// helper starts deeper with go, and takes a test, so the rule reads deeper's body too once helper is present.
func helper(t *testing.T) { go deeper(t) }

// deeper logs.
func deeper(t *testing.T) { t.Log("deeper") }

// Quiet starts quietHelper but takes no test, so the rule never hears it.
func Quiet() { go quietHelper() }

// quietHelper counts.
func quietHelper() int { return 1 }

// Standing takes a test and starts standingHelper with go.
func Standing(t *testing.T) { go standingHelper(t) }

// standingHelper logs.
func standingHelper(t *testing.T) { t.Log("standing") }

// Late takes a test and starts reached with go, which does not stand, so Late leaves by the caller direction.
func Late(t *testing.T) { go reached(t) }

// reached logs.
func reached(t *testing.T) { t.Log("reached") }

// Faulty starts afterFact with go and halts.
func Faulty(t *testing.T) {
	go afterFact(t)
	halt()
}

// afterFact logs.
func afterFact(t *testing.T) { t.Log("after") }

// halt never returns in this build.
func halt() { panic("halted") }

// RunsOutside hands outsideSub to Run by name, in a file that is no test file, which requireparallel never reads.
func RunsOutside(t *testing.T) { t.Run("outside", outsideSub) }

// outsideSub logs.
func outsideSub(t *testing.T) { t.Log("outside") }
