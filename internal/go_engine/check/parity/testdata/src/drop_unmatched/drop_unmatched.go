// Package drop_unmatched records a drop that does not happen: both sides find the call to shared.
package drop_unmatched

func shared() {}

func calls() {
	// Ruled a false positive, which the findings do not bear out.
	shared() // port drops: call to shared
}
