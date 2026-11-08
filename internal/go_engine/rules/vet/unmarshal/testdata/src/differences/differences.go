// Package differences holds where the port and the original disagree, each with its reason. The test reads each
// trailing comment that records the port finding a message as a finding only the port makes, and each that records
// the port dropping one as a finding only the original makes, which is wrong, each on that line with that message.
package differences

import (
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"io"
)

type record struct{ Name string }

type jsonDecoder = json.Decoder

type embedding struct{ *json.Decoder }

// A method expression takes the receiver as its first argument, so the value Decode fills is the second. The
// original reads the first, the receiver, and the port reads the second, the value decoded into, so a value there,
// which Decode fills nothing of, is found by the port alone, exactly as `decoder.Decode(value)` is found by both.
func methodExpressions(reader io.Reader) {
	var value record
	(*json.Decoder).Decode(json.NewDecoder(reader), value)   // port finds: call of Decode passes non-pointer
	(*gob.Decoder).Decode(gob.NewDecoder(reader), value)     // port finds: call of Decode passes non-pointer
	(*xml.Decoder).Decode(xml.NewDecoder(reader), value)     // port finds: call of Decode passes non-pointer
	(*jsonDecoder).Decode(json.NewDecoder(reader), value)    // port finds: call of Decode passes non-pointer
	((*json.Decoder).Decode)(json.NewDecoder(reader), value) // port finds: call of Decode passes non-pointer

	// The original judges the receiver, a struct value, and the value decoded into is a pointer, which Decode fills.
	embedding.Decode(embedding{json.NewDecoder(reader)}, &value) // port drops: call of Decode passes non-pointer

	pair := func() (*json.Decoder, any) { return json.NewDecoder(reader), &value }
	// The original judges the one argument written, a tuple of the Decoder and the value decoded into, which is no
	// pointer only because it is a tuple; the value decoded into is a pointer.
	(*json.Decoder).Decode(pair()) // port drops: call of Decode passes non-pointer

	// With a pointer or an interface decoded into, a method expression is clean on both sides.
	(*json.Decoder).Decode(json.NewDecoder(reader), &value)
	(*gob.Decoder).Decode(gob.NewDecoder(reader), any(value))
}
