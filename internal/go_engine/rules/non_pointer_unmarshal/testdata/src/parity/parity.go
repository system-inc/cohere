// Package parity calls each function and method with a target that is a pointer and with one that is not, in the
// shapes staticcheck's representation builds a target or a call in more than one way.
package parity

import (
	"encoding/json"
	"encoding/xml"
	"io"
)

type record struct {
	Name string
}

type recordPointer *record

type unmarshaler interface {
	Unmarshal() error
}

var decoded = json.Unmarshal(nil, record{})

func eachFunction(data []byte, reader io.Reader, start xml.StartElement) {
	var value record
	xml.Unmarshal(data, value)
	xml.Unmarshal(data, &value)
	xml.NewDecoder(reader).Decode(value)
	xml.NewDecoder(reader).Decode(&value)
	xml.NewDecoder(reader).DecodeElement(value, &start)
	xml.NewDecoder(reader).DecodeElement(&value, &start)
	json.Unmarshal(data, value)
	json.Unmarshal(data, &value)
	json.NewDecoder(reader).Decode(value)
	json.NewDecoder(reader).Decode(&value)
	json.Marshal(value)
	xml.NewDecoder(reader).Token()
}

func targetShapes(data []byte, given any, handler unmarshaler, condition bool) {
	var values []record
	json.Unmarshal(data, values)
	var table map[string]int
	json.Unmarshal(data, table)
	json.Unmarshal(data, 3)
	json.Unmarshal(data, "text")
	json.Unmarshal(data, nil)
	json.Unmarshal(data, given)
	json.Unmarshal(data, handler)
	json.Unmarshal(data, recordPointer(&record{}))
	json.Unmarshal(data, new(record))
	json.Unmarshal(data, &values)
	var boxed any = values
	json.Unmarshal(data, boxed)
	var boxedPointer any = &values
	json.Unmarshal(data, boxedPointer)
	var chosen any = values
	if condition {
		chosen = &values
	}
	json.Unmarshal(data, chosen)
	json.Unmarshal(data, func() {})
}

func callShapes(data []byte, reader io.Reader) {
	var value record
	defer json.Unmarshal(data, value)
	go json.Unmarshal(data, value)
	func() {
		json.Unmarshal(data, value)
	}()
	unmarshal := json.Unmarshal
	unmarshal(data, value)
	decoder := json.NewDecoder(reader)
	decoder.Decode(value)
	decode := decoder.Decode
	decode(value)
	json.Unmarshal(
		data,
		value,
	)
	if err := json.Unmarshal(data, value); err != nil {
		return
	}
	_ = decoded
}

func generic[T any](data []byte, value T, pointer *T) T {
	json.Unmarshal(data, value)
	json.Unmarshal(data, pointer)
	return value
}

func constrained[T ~[]int](data []byte, value T) {
	json.Unmarshal(data, value)
}
