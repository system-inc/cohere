package v

import "fmt"

// Label is one type in every build, whose method set only v's test grows by String (shape_windows_test.go), a file no
// build compiles into v itself.
type Label struct{ Text string }

// Labelled formats a Label, a fmt.Stringer in v's test alone.
func Labelled() string { return fmt.Sprint(Label{Text: "label"}) }
