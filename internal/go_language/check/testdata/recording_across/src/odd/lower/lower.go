// Package lower holds the callee an importer's odd slice mark rests on, for the stub differential across a package
// boundary (check's TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswerAcrossAPackage): Pairs needs its values in
// pairs, and Other is a function no question reaches.
package lower

// Pairs panics unless its values come in pairs.
func Pairs(values []string) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

// Other prints how many values it has, and nothing asks of it.
func Other(values []string) {
	println(len(values))
}
