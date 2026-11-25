// Package parity holds every function the check judges, each given a struct with no exported field and one with an
// exported field, structs of each shape the fields are counted through, embedded, blank, recursive, generic and
// anonymous, the methods that marshal each format on value and pointer receivers, and the shapes staticcheck's
// representation builds an argument or a call in more than one way.
package parity

import (
	"encoding/json"
	"encoding/xml"
	"io"
)

type hidden struct{ value int }

type shown struct{ Value int }

type blank struct{ _ int }

type empty struct{}

type embedsHidden struct{ hidden }

type embedsShown struct{ shown }

type embedsPointer struct{ *shown }

type embedsEmpty struct{ empty }

type embedsInterface struct{ io.Reader }

type embedsLowerInterface struct{ reader }

type reader interface{ Read([]byte) (int, error) }

type recursive struct {
	next *recursive
	self []recursive
}

type mutual struct{ *other }

type other struct{ *mutual }

type holder[Value any] struct{ value Value }

type exportedHolder[Value any] struct{ Value Value }

type named hidden

type alias = hidden

type pointerTo *hidden

type textValue struct{ value int }

func (textValue) MarshalText() ([]byte, error) { return nil, nil }

type textPointer struct{ value int }

func (*textPointer) MarshalText() ([]byte, error) { return nil, nil }

func (*textPointer) UnmarshalText([]byte) error { return nil }

type jsonValue struct{ value int }

func (jsonValue) MarshalJSON() ([]byte, error) { return nil, nil }

func (jsonValue) UnmarshalJSON([]byte) error { return nil }

type xmlValue struct{ value int }

func (xmlValue) MarshalXML(*xml.Encoder, xml.StartElement) error { return nil }

func (*xmlValue) UnmarshalXML(*xml.Decoder, xml.StartElement) error { return nil }

type wrongSignature struct{ value int }

func (wrongSignature) MarshalJSON() {}

type embedsMarshaler struct{ jsonValue }

type embedsPointerMarshaler struct{ *textPointer }

func eachFunction(encoderJSON *json.Encoder, encoderXML *xml.Encoder, decoderJSON *json.Decoder,
	decoderXML *xml.Decoder) {
	json.Marshal(hidden{})
	json.Marshal(shown{})
	json.MarshalIndent(hidden{}, "", " ")
	json.MarshalIndent(shown{}, "", " ")
	xml.Marshal(hidden{})
	xml.Marshal(shown{})
	xml.MarshalIndent(hidden{}, "", " ")
	xml.MarshalIndent(shown{}, "", " ")
	encoderJSON.Encode(hidden{})
	encoderJSON.Encode(shown{})
	encoderXML.Encode(hidden{})
	encoderXML.Encode(shown{})
	var first hidden
	var second shown
	json.Unmarshal(nil, &first)
	json.Unmarshal(nil, &second)
	xml.Unmarshal(nil, &first)
	xml.Unmarshal(nil, &second)
	decoderJSON.Decode(&first)
	decoderJSON.Decode(&second)
	decoderXML.Decode(&first)
	decoderXML.Decode(&second)
}

func shapes() {
	json.Marshal(blank{})
	json.Marshal(empty{})
	json.Marshal(&empty{})
	json.Marshal(embedsHidden{})
	json.Marshal(embedsShown{})
	json.Marshal(embedsPointer{})
	json.Marshal(embedsEmpty{})
	json.Marshal(embedsInterface{})
	json.Marshal(embedsLowerInterface{})
	json.Marshal(recursive{})
	json.Marshal(mutual{})
	json.Marshal(holder[int]{})
	json.Marshal(holder[shown]{})
	json.Marshal(exportedHolder[int]{})
	json.Marshal(named{})
	json.Marshal(alias{})
	json.Marshal(pointerTo(nil))
	json.Marshal(struct{ value int }{})
	json.Marshal(struct{ Value int }{})
	json.Marshal(&struct{ value int }{})
	pointer := &hidden{}
	json.Marshal(&pointer)
	json.Marshal([]hidden{})
	json.Marshal(map[string]hidden{})
	json.Marshal(hidden{}.value)
}

func methods() {
	json.Marshal(textValue{})
	json.Marshal(&textValue{})
	xml.Marshal(textValue{})
	json.Marshal(textPointer{})
	json.Marshal(&textPointer{})
	var text textPointer
	json.Unmarshal(nil, &text)
	json.Unmarshal(nil, text)
	json.Marshal(jsonValue{})
	xml.Marshal(jsonValue{})
	var value jsonValue
	json.Unmarshal(nil, &value)
	xml.Unmarshal(nil, &value)
	xml.Marshal(xmlValue{})
	json.Marshal(xmlValue{})
	var element xmlValue
	xml.Unmarshal(nil, &element)
	xml.Unmarshal(nil, element)
	json.Marshal(wrongSignature{})
	json.Marshal(embedsMarshaler{})
	xml.Marshal(embedsMarshaler{})
	json.Marshal(embedsPointerMarshaler{})
}

func generic[Value any, Structure ~struct{ value int }](value Value, structure Structure, held holder[Value]) {
	json.Marshal(value)
	json.Marshal(&value)
	json.Marshal(structure)
	json.Marshal(held)
	json.Marshal(holder[Value]{})
}

func interfaces(value any, condition bool) {
	json.Marshal(value)
	var held any = hidden{}
	json.Marshal(held)
	if condition {
		held = shown{}
	}
	json.Marshal(held)
}

func closures() {
	marshal := func() {
		json.Marshal(hidden{})
		xml.Marshal(shown{})
	}
	marshal()
}

var marshaled, marshalError = json.Marshal(hidden{})
