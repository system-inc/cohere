// Package dotted imports json and xml into its file's scope, so their Unmarshal and Decoder are called by bare name
// and still judged as the encoding packages' own.
package dotted

import (
	. "encoding/json"
	"encoding/xml"
	"io"
)

type record struct{ Name string }

func dotImported(data []byte, reader io.Reader) {
	var value record
	Unmarshal(data, value) // found
	Unmarshal(data, &value)
	NewDecoder(reader).Decode(value) // found
	NewDecoder(reader).Decode(&value)
	var decoder *Decoder = NewDecoder(reader)
	decoder.Decode(value)      // found
	xml.Unmarshal(data, value) // found
}
