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

var written = binary.Write(ioutil.Discard, binary.LittleEndian, true) // port finds: value of type bool cannot be used with binary.Write

var (
	fields  = binary.Write(ioutil.Discard, binary.LittleEndian, flags{})  // port finds: value of type example.com/package_initializers.flags cannot be used with binary.Write
	integer = binary.Write(ioutil.Discard, binary.LittleEndian, int(0))   // port finds: value of type int cannot be used with binary.Write
	words   = binary.Write(ioutil.Discard, binary.LittleEndian, []bool{}) // port finds: value of type []bool cannot be used with binary.Write
)

func inBody() {
	binary.Write(ioutil.Discard, binary.LittleEndian, false) // port finds: value of type bool cannot be used with binary.Write
}
