// Package drop_unmatched records a drop that does not happen: both sides find the call to shared.
package drop_unmatched

func shared() {}

// calls holds a drop ruled here, which the findings do not bear out.
func calls() {
	shared() // port drops: call to shared
}
