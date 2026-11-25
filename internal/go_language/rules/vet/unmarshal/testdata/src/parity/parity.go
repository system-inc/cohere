// Package parity holds every branch of unmarshal's judgment, each encoding's function and method with a value that
// is found beside one that is clean, and the shapes most likely to read differently: aliases, defined pointer and
// interface types, type parameters, method values and expressions, promoted methods, closures and package-level
// initializers. The differential test runs the original and the port over each and compares. Each call that should
// be found says so in a trailing comment; every other call is clean.
package parity

import (
	"encoding/asn1"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"io"
)

type record struct{ Name string }

// An alias of a pointer type, a defined pointer type, and an alias of a struct type.
type (
	recordPointer = *record
	namedPointer  *record
	recordAlias   = record
)

// A defined interface, an alias of `any`, and a defined map type.
type (
	named    interface{ Label() string }
	anything = any
	mapping  map[string]int
)

type box[T any] struct{ value T }

// An alias of each encoding's Decoder, so the receiver is reached through an alias.
type (
	jsonDecoder = json.Decoder
	gobDecoder  = gob.Decoder
	xmlDecoder  = xml.Decoder
)

// decoder is any Decoder seen through an interface, whose method is no static callee.
type decoder interface{ Decode(value any) error }

// embedding promotes json's Decode through an embedded pointer, embeddingValue through an embedded value, and
// embeddingInterface through an embedded interface, which leaves no static callee.
type (
	embedding          struct{ *json.Decoder }
	embeddingValue     struct{ xml.Decoder }
	embeddingInterface struct{ decoder }
)

// Decoder and Unmarshal are lookalikes of this package's own, with the names and signatures the pass reads, and are
// never judged, since only the encoding packages' are.
type Decoder struct{}

func (*Decoder) Decode(value any) error { return nil }

func Unmarshal(data []byte, value any) error { return nil }

// unmarshaler has an Unmarshal method, which is a method, so it is never json's Unmarshal function.
type unmarshaler struct{}

func (unmarshaler) Unmarshal(data []byte, value any) error { return nil }

// Each encoding's Unmarshal and Decode with a struct value, then a pointer to it, then the other functions and
// methods of the same packages, which decode into their argument but are not the pass's.
func eachEncoding(data []byte, reader io.Reader) {
	var value record
	json.Unmarshal(data, value) // found
	json.Unmarshal(data, &value)
	xml.Unmarshal(data, value) // found
	xml.Unmarshal(data, &value)
	asn1.Unmarshal(data, value) // found
	asn1.Unmarshal(data, &value)
	json.NewDecoder(reader).Decode(value) // found
	json.NewDecoder(reader).Decode(&value)
	gob.NewDecoder(reader).Decode(value) // found
	gob.NewDecoder(reader).Decode(&value)
	xml.NewDecoder(reader).Decode(value) // found
	xml.NewDecoder(reader).Decode(&value)

	asn1.UnmarshalWithParams(data, value, "")
	xml.NewDecoder(reader).DecodeElement(value, nil)
	json.NewDecoder(reader).Token()
	json.Valid(data)
}

// The kinds of value that are neither pointer nor interface, and the kinds that are, through every spelling.
func argumentKinds(data []byte, reader io.Reader) {
	var (
		value        record
		pointer      recordPointer
		defined      namedPointer
		aliased      recordAlias
		interfaced   named
		anyValue     anything
		mapped       mapping
		boxed        box[int]
		boxedPointer *box[string]
		function     func()
		channel      chan int
		text         string
		number       int
		slice        []record
		array        [2]record
		emptyStruct  struct{}
		empty        interface{}
		errorValue   error
	)
	json.Unmarshal(data, pointer)
	json.Unmarshal(data, defined)
	json.Unmarshal(data, aliased) // found
	json.Unmarshal(data, &aliased)
	json.Unmarshal(data, interfaced)
	json.Unmarshal(data, anyValue)
	json.Unmarshal(data, mapped) // found
	json.Unmarshal(data, &mapped)
	json.Unmarshal(data, boxed) // found
	json.Unmarshal(data, boxedPointer)
	json.Unmarshal(data, function) // found
	json.Unmarshal(data, channel)  // found
	json.Unmarshal(data, text)     // found
	json.Unmarshal(data, number)   // found
	json.Unmarshal(data, slice)    // found
	json.Unmarshal(data, array)    // found
	json.Unmarshal(data, &array[0])
	json.Unmarshal(data, emptyStruct) // found
	json.Unmarshal(data, empty)
	json.Unmarshal(data, errorValue)
	json.Unmarshal(data, (value)) // found
	json.Unmarshal(data, (&value))
	json.Unmarshal(data, 1)      // found
	json.Unmarshal(data, "text") // found
	json.Unmarshal(data, nil)    // found
	json.Unmarshal(data, any(value))
	json.Unmarshal(data, *pointer) // found
	json.Unmarshal(data, record{}) // found
	json.Unmarshal(data, &record{})
	json.Unmarshal(data, new(record))
	json.Unmarshal(data, []byte(nil)) // found

	json.NewDecoder(reader).Decode(aliased) // found
	json.NewDecoder(reader).Decode(defined)
	gob.NewDecoder(reader).Decode(boxed) // found
	gob.NewDecoder(reader).Decode(interfaced)
	xml.NewDecoder(reader).Decode(mapped) // found
	xml.NewDecoder(reader).Decode(anyValue)
	xml.NewDecoder(reader).Decode(nil) // found
}

// Type parameters are never judged, whatever their constraint, but a type built from one is.
func typeParameters[Value any, Pointer ~*record, Concrete record | int, Mapped ~map[string]int](
	data []byte,
	reader io.Reader,
	value Value,
	pointer Pointer,
	concrete Concrete,
	mapped Mapped,
) {
	json.Unmarshal(data, value)
	json.Unmarshal(data, &value)
	json.Unmarshal(data, pointer)
	json.Unmarshal(data, concrete)
	json.Unmarshal(data, mapped)
	json.Unmarshal(data, []Value{})          // found
	json.Unmarshal(data, box[Value]{})       // found
	json.Unmarshal(data, map[string]Value{}) // found
	json.NewDecoder(reader).Decode(value)
	gob.NewDecoder(reader).Decode(concrete)
	xml.NewDecoder(reader).Decode(box[Pointer]{}) // found
}

// A generic function called with each kind, judged once in its body and never at its call.
func callsTypeParameters(data []byte, reader io.Reader) {
	typeParameters(data, reader, record{}, &record{}, 1, mapping{})
	typeParameters[*record, *record, record, map[string]int](data, reader, nil, nil, record{}, nil)
}

// Receivers reached through aliases, promotion, method values and method expressions.
func receivers(data []byte, reader io.Reader) {
	var value record
	var throughAlias *jsonDecoder = json.NewDecoder(reader)
	throughAlias.Decode(value)                                        // found
	(*gobDecoder)(gob.NewDecoder(reader)).Decode(value)               // found
	(*xmlDecoder)(xml.NewDecoder(reader)).Decode(value)               // found
	embedding{json.NewDecoder(reader)}.Decode(value)                  // found
	(&embeddingValue{Decoder: *xml.NewDecoder(reader)}).Decode(value) // found
	embeddingInterface{json.NewDecoder(reader)}.Decode(value)

	var through decoder = json.NewDecoder(reader)
	through.Decode(value)

	// A method value is a function value, so its call has no static callee.
	decode := json.NewDecoder(reader).Decode
	decode(value)
	unmarshal := json.Unmarshal
	unmarshal(data, value)

	// A method expression takes the receiver first, so the decoded argument is its second. With a pointer there,
	// both are clean; with a value there, the port alone finds it (testdata/src/differences).
	(*json.Decoder).Decode(json.NewDecoder(reader), &value)
	(*gob.Decoder).Decode(gob.NewDecoder(reader), &value)
	// Through a promoted method's expression, the receiver is a struct value, which the original judges, and so is
	// the value decoded into, which the port judges, so both find it at the same place in the same words.
	embedding.Decode(embedding{json.NewDecoder(reader)}, value) // found

	// A parenthesized callee is still the function itself.
	(json.Unmarshal)(data, value)           // found
	(json.NewDecoder(reader).Decode)(value) // found

	// Lookalikes in this package, and a method named Unmarshal.
	Unmarshal(data, value)
	(&Decoder{}).Decode(value)
	unmarshaler{}.Unmarshal(data, value)
}

// A call that spreads another's results writes too few arguments to judge.
func spread(data []byte, reader io.Reader) {
	pair := func() ([]byte, any) { return data, record{} }
	json.Unmarshal(pair())
	single := func() any { return record{} }
	json.NewDecoder(reader).Decode(single())
	json.NewDecoder(reader).Decode(record{}) // found
}

// Calls nested in closures, deferred, in goroutines, as arguments to other calls, and in each other.
func nested(data []byte, reader io.Reader) {
	var value record
	func() {
		json.Unmarshal(data, value) // found
	}()
	defer json.Unmarshal(data, value)                                    // found
	go gob.NewDecoder(reader).Decode(value)                              // found
	_ = []error{xml.Unmarshal(data, value), xml.Unmarshal(data, &value)} // found
	// Found once, the inner call: the outer decodes into the error the inner returns, an interface.
	_ = json.Unmarshal(data, json.Unmarshal(data, value)) // found
}

// A call in a package-level initializer is judged as one in a function.
var initialized = json.Unmarshal(nil, record{}) // found

var initializedClean = json.Unmarshal(nil, &record{})
