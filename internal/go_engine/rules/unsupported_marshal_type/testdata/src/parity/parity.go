// Package parity holds the shapes the original and the port must judge alike: each of the six functions the check
// knows, called with a value it cannot marshal and with one it can; a value reached as a variable, a parameter, a
// field, a pointer, a conversion, a literal, a function or a method value; types named in this package and in
// others; the struct tags, embeddings and marshaler methods that change what is walked; and the types
// staticcheck's representation leaves generic.
package parity

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"time"
)

type Event struct {
	Name    string
	Done    chan struct{}
	Handler func()
}

type Tagged struct {
	Name    string
	Done    chan struct{} `json:"-" xml:"-"`
	Handler func()        `json:"-" xml:"-"`
	Dash    chan int      `json:"-,"`
	Empty   chan int      `json:",omitempty"`
}

type hidden struct {
	Signals chan os.Signal
}

type Outer struct {
	hidden
}

type Pointed struct {
	Inner *Event
}

type ValueJSON chan int

func (ValueJSON) MarshalJSON() ([]byte, error) { return nil, nil }

type PointerJSON func()

func (*PointerJSON) MarshalJSON() ([]byte, error) { return nil, nil }

type ValueXML map[string]string

func (ValueXML) MarshalXML(*xml.Encoder, xml.StartElement) error { return nil }

type Holder struct {
	JSON PointerJSON
}

type Cycle *Cycle

type Cyclic struct {
	Next Cycle
}

type Conflicting struct {
	First  string `xml:"a>b"`
	Second string `xml:"a"`
}

type Box[T any] struct {
	Value T
}

type Pair[K comparable, V any] struct {
	Entries map[K]V
}

func (event Event) Method() {}

func everyFunction(writer io.Writer, event Event, tagged Tagged) {
	json.Marshal(event.Done)
	json.Marshal(tagged)
	json.MarshalIndent(event.Handler, "", "  ")
	json.MarshalIndent(tagged.Name, "", "  ")
	xml.Marshal(event)
	xml.Marshal(tagged.Name)
	xml.MarshalIndent(event.Done, "", "  ")
	xml.MarshalIndent(tagged, "", "  ")
	json.NewEncoder(writer).Encode(event)
	json.NewEncoder(writer).Encode(tagged.Name)
	xml.NewEncoder(writer).Encode(event.Handler)
	xml.NewEncoder(writer).Encode(tagged)
}

func values(event Event, pointer *Event, values []Event, mapped map[string]Event) {
	json.Marshal(pointer)
	json.Marshal(&event)
	json.Marshal(values)
	json.Marshal(mapped)
	xml.Marshal(pointer)
	xml.Marshal(values)
	json.Marshal(any(event))
	json.Marshal(Event{})
	json.Marshal(&Event{})
	json.Marshal(func() {})
	json.Marshal(event.Method)
	json.Marshal(&values)
	json.Marshal(nil)
	xml.Marshal(nil)
	var boxed any = event.Done
	json.Marshal(boxed)
	json.Marshal([2]func(){})
	json.Marshal([]byte{})
	json.Marshal([]chan int{})
	json.Marshal(new(func()))
}

func otherPackages(ticker *time.Ticker, signals chan os.Signal) {
	json.Marshal(signals)
	xml.Marshal(signals)
	json.Marshal(ticker)
	xml.Marshal(*ticker)
	json.Marshal(Outer{})
	xml.Marshal(Outer{})
	json.Marshal(time.Time{})
	xml.Marshal(time.Duration(0))
}

func nested() {
	json.Marshal(Pointed{})
	xml.Marshal(Pointed{})
	json.Marshal(struct{ Handlers []func() }{})
	xml.Marshal(struct{ Handlers map[string]int }{})
	json.Marshal(map[bool]string{})
	json.Marshal(map[float64]string{})
	json.Marshal(map[int64]string{})
	json.Marshal(map[time.Duration]func(){})
}

func marshalers(holder Holder, pointerJSON PointerJSON) {
	json.Marshal(ValueJSON(nil))
	xml.Marshal(ValueJSON(nil))
	json.Marshal(pointerJSON)
	json.Marshal(&pointerJSON)
	json.Marshal(holder)
	json.Marshal(&holder)
	xml.Marshal(ValueXML(nil))
	json.Marshal(ValueXML(nil))
	json.Marshal(json.RawMessage{})
	xml.Marshal(xml.Name{})
}

func cyclesAndTags() {
	xml.Marshal(Cyclic{})
	xml.Marshal(&Cyclic{})
	json.Marshal(Cyclic{})
	xml.Marshal(Conflicting{})
	json.Marshal(Conflicting{})
}

func generic[T any, C chan int](value T, channel C, boxed Box[T]) {
	json.Marshal(value)
	xml.Marshal(value)
	json.Marshal(channel)
	xml.Marshal(channel)
	json.Marshal(boxed)
	json.Marshal(Box[C]{})
	json.Marshal(Box[chan T]{})
}

func instantiated() {
	json.Marshal(Box[chan int]{})
	xml.Marshal(Box[func()]{})
	json.Marshal(Box[string]{})
	json.Marshal(Pair[string, func()]{})
	json.Marshal(Pair[any, int]{})
	generic[int, chan int](0, nil, Box[int]{})
}

func local() {
	type local struct {
		Callback func()
	}
	json.Marshal(local{})
	xml.Marshal([]local{})
}
