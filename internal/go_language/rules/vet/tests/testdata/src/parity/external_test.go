package parity_test

import (
	"fmt"

	"other"
	"parity"
)

var _ = fmt.Sprint

// The external test package declares none of these, so each is looked up in its imports: parity, other and fmt. Buffer
// is declared by both parity and other, so a member either has is found.

func ExampleBuffer() {}

func ExampleBuffer_Grow() {}

func ExampleBuffer_Reset() {}

func ExampleBuffer_Neither() {}

func ExampleList_Push() {}

func ExampleHelper() {}

func ExampleSprint() {}

func ExampleMissing() {}

func ExamplePair_Pop() {}

func ExampleLocal() {}

func ExampleLocal_Method() {}

func ExampleLocal_Gone() {}

// Local is declared here, so it is found in the package itself and its imports are not searched.
type Local struct{}

func (Local) Method() {}

var _ parity.Buffer
var _ other.Buffer
