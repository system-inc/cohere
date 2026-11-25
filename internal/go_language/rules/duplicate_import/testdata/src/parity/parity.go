// Package parity holds every import shape `ST1019` reads, each found and each near miss, for the original and the port
// to judge alike.
package parity

import (
	// fmt is imported three times, reported at the first import with the other two related.
	"fmt"
	format "fmt"
	printing "fmt"

	// A blank import beside one other import of its path is left alone.
	"errors"
	_ "errors"

	// A blank import beside two others leaves the two, which are reported.
	"bytes"
	_ "bytes"
	buffers "bytes"

	// A dot import is an import like any other.
	. "strings"
	text "strings"

	// unsafe is never reported, however many times it is imported.
	"unsafe"
	pointers "unsafe"

	// Two packages of the same last element are different paths.
	"math/rand"
	randomV2 "math/rand/v2"
)

// os is imported in a declaration of its own and in a second one, and the two are one file's imports.
import "os"

import system "os"

var (
	_ = fmt.Sprint
	_ = format.Sprint
	_ = printing.Sprint
	_ = errors.New
	_ = bytes.Clone
	_ = buffers.Clone
	_ = ToUpper
	_ = text.ToUpper
	_ = unsafe.Sizeof(0)
	_ = pointers.Sizeof(0)
	_ = rand.Int
	_ = randomV2.Int
	_ = os.Getenv
	_ = system.Getenv
)
