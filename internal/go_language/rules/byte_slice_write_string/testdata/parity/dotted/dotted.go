package dotted

import . "io"

// Found: io.WriteString by name alone, through a dot import.
func dotted(writer Writer, bytes []byte) {
	WriteString(writer, string(bytes))
}
