package parity

// A line directive naming a test file leaves this a file that is not one, since a test file is judged by the name it
// was parsed from, and its dot import is reported.
//line directive_test.go:1
import . "unicode/utf8"

var _ = RuneLen
