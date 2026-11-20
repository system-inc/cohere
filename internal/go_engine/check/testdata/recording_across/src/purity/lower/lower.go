// Package lower holds the callees an importer's purity rests on, for the stub differential across a package boundary
// (check's TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswerAcrossAPackage): Middle is pure, by way of inner, and
// Other is a function no question reaches.
package lower

// inner doubles value.
func inner(value int) int {
	return value * 2
}

// Middle is one more than inner's, pure as inner is.
func Middle(value int) int {
	return inner(value) + 1
}

// Other is one less than value, and nothing asks of it.
func Other(value int) int {
	return value - 1
}
