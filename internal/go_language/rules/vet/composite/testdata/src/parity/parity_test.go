package parity_test

import (
	"testing"

	"parity"
	"parity/library"
)

// An external test's literal of its package's struct is local, since foo_test counts as foo.
var _ = parity.Local{1}

var _ = parity.LocalPair{"name", 1}

// Its literal of another package's struct is judged as any.
var _ = library.Struct{1}

// The test-main structs are allowed.
var _ = []testing.InternalTest{{"Test", nil}}
