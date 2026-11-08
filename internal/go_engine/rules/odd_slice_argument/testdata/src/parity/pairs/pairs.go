// Package pairs declares functions that need their slices of even length, for parity to call from another package,
// so the marks reach it as facts.
package pairs

// Pairs panics unless its values come in pairs.
func Pairs(values ...string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// Forward passes its values on to Pairs, so it needs them in pairs too.
func Forward(values ...string) {
	Pairs(values...)
}
