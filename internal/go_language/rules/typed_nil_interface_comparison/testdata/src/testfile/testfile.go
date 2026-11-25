// Package testfile holds a function that never returns a nil interface but always holds a typed nil, compared with nil
// only in a test file, where the comparison is never reported, so judging the function's nilness is work for nothing.
package testfile

type failure struct{}

func (*failure) Error() string { return "failure" }

func typedNil() error {
	println("not trivial")
	var value *failure
	return value
}
