// Package parity holds values of every kind of type binary.Write is given, both those it can encode and those it
// cannot, through the shapes staticcheck's representation builds an argument or a call in more than one way. The
// module is Go 1.20's, older than Go 1.21, so a file tagged with a Go version reads that version's standard library,
// older or newer: tagged_older.go and tagged_newer.go hold the bools each judges differently, and this file the bools
// the module's own version judges.
package parity

import (
	"encoding/binary"
	"io"
	"io/ioutil"
	"unsafe"
)

type flags struct {
	first  bool
	second int8
}

type fixed struct {
	first  int32
	second [4]uint16
	third  struct{ value float64 }
	_      int64
}

type sized struct {
	first int32
	count int
}

type empty struct{}

type named int64

type wide uint

type alias = int32

type wideAlias = uint

type bytes []byte

type matrix [2][2]float32

type holder[Value any] struct {
	value Value
}

func basics(writer io.Writer, order binary.ByteOrder) {
	binary.Write(writer, order, int(0))
	binary.Write(writer, order, uint(0))
	binary.Write(writer, order, uintptr(0))
	binary.Write(writer, order, int8(0))
	binary.Write(writer, order, int16(0))
	binary.Write(writer, order, int32(0))
	binary.Write(writer, order, int64(0))
	binary.Write(writer, order, uint8(0))
	binary.Write(writer, order, uint16(0))
	binary.Write(writer, order, uint32(0))
	binary.Write(writer, order, uint64(0))
	binary.Write(writer, order, float32(0))
	binary.Write(writer, order, float64(0))
	binary.Write(writer, order, complex64(0))
	binary.Write(writer, order, complex128(0))
	binary.Write(writer, order, "text")
	binary.Write(writer, order, true)
	binary.Write(writer, order, unsafe.Pointer(nil))
	binary.Write(writer, order, 1)
	binary.Write(writer, order, 1.5)
	binary.Write(writer, order, 'r')
	binary.Write(writer, order, nil)
}

func containers(writer io.Writer, order binary.ByteOrder) {
	var integers []int
	var words []int32
	var pointers []*int32
	var integerArray [3]int
	var wordArray [3]int32
	var grid matrix
	var table map[string]int8
	var wideTable map[string]int
	var channel chan int8
	var wideChannel chan int
	var function func()
	var text []string
	var buffer bytes
	binary.Write(writer, order, integers)
	binary.Write(writer, order, words)
	binary.Write(writer, order, pointers)
	binary.Write(writer, order, integerArray)
	binary.Write(writer, order, wordArray)
	binary.Write(writer, order, grid)
	binary.Write(writer, order, &grid)
	binary.Write(writer, order, table)
	binary.Write(writer, order, wideTable)
	binary.Write(writer, order, channel)
	binary.Write(writer, order, wideChannel)
	binary.Write(writer, order, function)
	binary.Write(writer, order, text)
	binary.Write(writer, order, buffer)
	binary.Write(writer, order, &buffer)
	binary.Write(writer, order, &integers)
	binary.Write(writer, order, &words)
}

func pointers(writer io.Writer, order binary.ByteOrder) {
	var word int32
	var integer int
	var pointer *int32
	var flag bool
	binary.Write(writer, order, &word)
	binary.Write(writer, order, &integer)
	binary.Write(writer, order, &pointer)
	binary.Write(writer, order, &flag)
	binary.Write(writer, order, pointer)
}

func structures(writer io.Writer, order binary.ByteOrder) {
	binary.Write(writer, order, flags{})
	binary.Write(writer, order, &flags{})
	binary.Write(writer, order, fixed{})
	binary.Write(writer, order, []fixed{})
	binary.Write(writer, order, sized{})
	binary.Write(writer, order, [2]sized{})
	binary.Write(writer, order, empty{})
	binary.Write(writer, order, struct{ value *int32 }{})
	binary.Write(writer, order, struct{ value []int32 }{})
	binary.Write(writer, order, struct{ value any }{})
	binary.Write(writer, order, holder[int32]{})
	binary.Write(writer, order, holder[int]{})
	binary.Write(writer, order, holder[bool]{})
}

func namedTypes(writer io.Writer, order binary.ByteOrder) {
	binary.Write(writer, order, named(0))
	binary.Write(writer, order, wide(0))
	binary.Write(writer, order, alias(0))
	binary.Write(writer, order, wideAlias(0))
	binary.Write(writer, order, []wideAlias{})
}

func interfaces(writer io.Writer, order binary.ByteOrder, value any, reader io.Reader) {
	binary.Write(writer, order, value)
	binary.Write(writer, order, reader)
	held := any(int(0))
	binary.Write(writer, order, held)
}

func generic[Value any, Number ~int32 | ~int](writer io.Writer, order binary.ByteOrder, value Value, number Number) {
	binary.Write(writer, order, value)
	binary.Write(writer, order, number)
	binary.Write(writer, order, &value)
	binary.Write(writer, order, []Value{})
	binary.Write(writer, order, holder[Value]{})
}

func integerAndOrder() (io.Writer, binary.ByteOrder, int) {
	return ioutil.Discard, binary.BigEndian, 0
}

func results() {
	binary.Write(integerAndOrder())
}

func closures(writer io.Writer, order binary.ByteOrder) {
	write := func() {
		binary.Write(writer, order, uint(0))
		binary.Write(writer, order, false)
	}
	write()
}

func meeting(writer io.Writer, order binary.ByteOrder, condition bool) {
	var data any = int32(0)
	if condition {
		data = int(0)
	}
	binary.Write(writer, order, data)
}

var written = binary.Write(ioutil.Discard, binary.LittleEndian, int(0))
