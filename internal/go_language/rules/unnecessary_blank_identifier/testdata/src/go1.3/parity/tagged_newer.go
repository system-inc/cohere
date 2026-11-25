//go:build go1.22

package parity

// A file tagged newer than the module has its tag's language, where a range may leave out its variables, so both
// report each range here, which the module's own version holds back in parity.go.
func taggedNewer(m map[string]int) {
	var key string
	for _ = range m {
	}
	for _, _ = range m {
	}
	for key, _ = range m {
	}
	_ = key
}
