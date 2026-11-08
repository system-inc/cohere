package parity

// A path imported once in each of two files is imported once in each, and neither file reports it.
import "fmt"

var _ = fmt.Sprint
