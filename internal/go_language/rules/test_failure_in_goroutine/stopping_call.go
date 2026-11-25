package test_failure_in_goroutine

import (
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// StoppingCall is one call among a function's own instructions to a method of testing's common type that stops the
// test, as the fact resolver holds it of that function: what a report of it needs and nothing of the run that read it,
// so an answer a store gives back, or a test holds, reports what one computed in the run reports.
type StoppingCall struct {
	// Method is the name of the method called, as FailNow.
	Method string
	// HasSyntax is whether the call has syntax, where a report puts its related entry. A call with none, as a wrapper's
	// call of the method it wraps, is reported without the entry, and its File, Start and End are zero.
	HasSyntax bool
	// File is the name the file holding the call's syntax was parsed from, and Start and End the byte offsets in it
	// where the syntax starts and ends.
	File  string
	Start int
	End   int
}

// newStoppingCall is call, a call to the stopping method name, as the resolver holds it, its syntax placed by the file
// set of the program it was built in.
func newStoppingCall(call *intermediate_representation.Call, name string) StoppingCall {
	source := call.Source()
	if source == nil {
		return StoppingCall{Method: name}
	}
	file := call.Parent().Program.FileSet.File(source.Pos())
	if file == nil {
		// Invariant: a call's syntax is a node of a file its package was built from, which the program's file set
		// holds, so a call whose syntax is in no file is a mistake in the representation.
		panic("test_failure_in_goroutine: the call to T." + name + " in " + call.Parent().String() +
			" has syntax in no file of its program")
	}
	return StoppingCall{
		Method:    name,
		HasSyntax: true,
		File:      file.Name(),
		Start:     file.Offset(source.Pos()),
		End:       file.Offset(source.End()),
	}
}
