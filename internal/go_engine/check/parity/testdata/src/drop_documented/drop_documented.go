// Package drop_documented holds two drops and a find in a function whose doc comment rules them all. A drop's reason
// is its own, on the line directly above it, so the drop with one there passes and the drop with none fails, while
// the find the doc rules passes.
package drop_documented

func gained() {}
func lost()   {}
func shared() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly drops lost and finds gained.
func calls() {
	// Ruled a false positive with the finding in hand: a call to lost is harmless here.
	lost()   // port drops: call to lost
	gained() // port finds: call to gained
	shared()
	lost() // port drops: call to lost
}
