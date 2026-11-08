// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original walks the calls staticcheck's type index records of io.WriteString
// and matches its pattern through parentheses at every level, binding the converted value with its parentheses
// removed, and the port walks every call, so these calls are parenthesized everywhere at once, where a reach of the
// port's own would show as a finding one side makes and the other does not.
package differences

import "io"

type small byte

// Found by both, twice: parentheses at every level, around a []byte and around a slice that converts only from Go
// 1.18.
func everywhere(writer io.Writer, bytes []byte, values []small) {
	(io.WriteString)(writer, (string)((bytes)))
	(io.WriteString)((writer), ((string)((values))))
}

// Found by neither: three arguments to a function of the package's own named like it.
func notIo(bytes []byte) {
	writeString(nil, string(bytes), 0)
}

func writeString(writer io.Writer, text string, count int) {}
