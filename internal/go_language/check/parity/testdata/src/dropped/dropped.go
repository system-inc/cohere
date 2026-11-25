// Package dropped holds a call to lost, which only the original finds, ruled a false positive and recorded as
// dropped.
package dropped

func lost() {}

func calls() {
	// Ruled a false positive with the finding in hand: a call to lost is harmless here.
	lost() // port drops: call to lost
}
