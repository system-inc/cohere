// Package drop_unmatched records a drop that does not happen: both sides find the call to shared.
package drop_unmatched

func shared() {}

func calls() {
	shared() // port drops: call to shared
}
