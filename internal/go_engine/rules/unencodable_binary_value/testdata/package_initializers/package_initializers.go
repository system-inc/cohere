// Package package_initializers holds the one place the port and the original differ: binary.Write called in a
// package-level variable's initializer, which runs in the package initializer, a function in no file. The original
// reads the standard library's version from the file of the function a call is in, and panics finding none when the
// value's type holds a bool, so it cannot run here at all. The port reads it from the file the call is in. The module
// is Go 1.7's, whose standard library could not encode a bool, so every bool here is a finding, recorded on its line,
// as is the int given in the same initializers, which the original would have found too.
package package_initializers

import (
	"encoding/binary"
	"io/ioutil"
)

type flags struct {
	first  bool
	second int8
}

// written is ruled here: Go 1.7's standard library cannot encode a bool, and the port reads that version from the
// file the call is in, where the original asks the package initializer's file, finds none and panics.
var written = binary.Write(ioutil.Discard, binary.LittleEndian, true) // port finds: value of type bool cannot be used with binary.Write

// The group is ruled here: flags holds a bool and []bool is of them, which Go 1.7 cannot encode, and int has no
// fixed size, which the original would have found too, were it not to panic on the package.
var (
	fields  = binary.Write(ioutil.Discard, binary.LittleEndian, flags{})  // port finds: value of type example.com/package_initializers.flags cannot be used with binary.Write
	integer = binary.Write(ioutil.Discard, binary.LittleEndian, int(0))   // port finds: value of type int cannot be used with binary.Write
	words   = binary.Write(ioutil.Discard, binary.LittleEndian, []bool{}) // port finds: value of type []bool cannot be used with binary.Write
)

// inBody is ruled here: a bool in a function body, which the original would find too, were it not lost to the panic
// with the rest of the package.
func inBody() {
	binary.Write(ioutil.Discard, binary.LittleEndian, false) // port finds: value of type bool cannot be used with binary.Write
}
