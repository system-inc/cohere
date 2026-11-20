// Package p is the package a later run reloads from source and answers from its entry (#z9fze0t): Mid is pure only
// where platformStep is, and a run that writes p's entry with only the answers its questions computed keeps Mid's and
// not inner's, on which it rests, judged inside Mid's judgment.
package p

// Limit is read by q.
const Limit = 3

// inner calls what each platform declares.
func inner(value int) int {
	return platformStep(value) + 1
}

// Mid names only inner.
func Mid(value int) int {
	return inner(value) * 2
}

// Other is pure in every build, and nothing asks of it until q discards its result.
func Other(value int) int {
	return value * 9
}

// User discards Mid's result, a finding only where Mid is pure.
func User() int {
	Mid(2)
	return 1
}
