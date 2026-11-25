// Package parity holds buffers for each encoder the original judges: one value, one value reached through a phi or a
// loop, slices of one array from one bound written as a constant, a variable or no bound at all, from bounds that
// differ or that a phi gives, slices of arrays that differ, nil, and the shapes staticcheck's representation builds
// the call in more than one way.
package parity

import (
	"encoding/ascii85"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
)

const start = 1

var shared = make([]byte, 16)

func init() {
	hex.Encode(shared, shared)
}

func everyEncoder(buffer []byte, other []byte) {
	ascii85.Encode(buffer, buffer)
	base32.StdEncoding.Encode(buffer, buffer)
	base32.HexEncoding.Encode(buffer, buffer)
	base64.StdEncoding.Encode(buffer, buffer)
	base64.URLEncoding.Encode(buffer, buffer)
	base64.RawStdEncoding.Encode(buffer, buffer)
	hex.Encode(buffer, buffer)
	ascii85.Encode(buffer, other)
	base32.StdEncoding.Encode(buffer, other)
	base64.StdEncoding.Encode(buffer, other)
	hex.Encode(buffer, other)
	_ = base64.StdEncoding.EncodeToString(buffer)
	_, _, _ = ascii85.Decode(buffer, buffer, true)
	_, _ = hex.Decode(buffer, buffer)
}

func nils(buffer []byte) {
	hex.Encode(nil, buffer)
	hex.Encode(buffer, nil)
	hex.Encode(nil, nil)
	hex.Encode([]byte(nil), []byte(nil))
	var empty []byte
	hex.Encode(empty, empty)
}

func bounds(buffer []byte, array *[8]byte, low int, high int) {
	hex.Encode(buffer[1:], buffer[1:])
	hex.Encode(buffer[0:], buffer[:])
	hex.Encode(buffer[:], buffer[:])
	hex.Encode(buffer[:4], buffer[:2])
	hex.Encode(buffer[start:], buffer[1:])
	hex.Encode(buffer[low:], buffer[low:])
	hex.Encode(buffer[low:], buffer[high:])
	hex.Encode(buffer[low:high], buffer[low:])
	hex.Encode(buffer[low+1:], buffer[low+1:])
	hex.Encode(buffer[2:], buffer[1:])
	hex.Encode(buffer[1:3], buffer[2:4])
	hex.Encode(buffer[1:2:3], buffer[1:])
	hex.Encode(array[:], array[:])
	hex.Encode(array[2:], array[2:])
	hex.Encode(array[2:], array[3:])
	hex.Encode(buffer, buffer[:])
	hex.Encode(buffer[:], buffer)
	var unsigned uint = 1
	hex.Encode(buffer[unsigned:], buffer[1:])
	hex.Encode(buffer[int8(1):], buffer[1:])
}

func arrays(first []byte, second []byte, array [8]byte) {
	hex.Encode(first[1:], second[1:])
	hex.Encode(array[:], array[:])
	copied := array
	hex.Encode(array[:], copied[:])
	text := "text"
	hex.Encode([]byte(text), []byte(text))
	made := make([]byte, 4)
	hex.Encode(made, make([]byte, 4))
}

func phis(condition bool, buffer []byte, other []byte, low int) {
	chosen := buffer
	if condition {
		chosen = other
	}
	hex.Encode(chosen, buffer)
	hex.Encode(chosen, chosen)
	agreeing := buffer
	if condition {
		agreeing = buffer
	}
	hex.Encode(agreeing, buffer)
	first := low
	second := low + 1
	if condition {
		first = low + 2
		second = low + 3
	}
	// Each bound is a phi whose edges differ, which Flatten makes nil, so both judge the two one bound and mark
	// them, though they never meet.
	hex.Encode(buffer[first:], buffer[second:])
	sliced := buffer[1:]
	if condition {
		sliced = buffer[1:]
	}
	hex.Encode(sliced, buffer[1:])
	one := buffer
	two := other
	if condition {
		one = other
		two = buffer
	}
	hex.Encode(one[1:], two[1:])
	maybeNil := buffer
	if condition {
		maybeNil = nil
	}
	hex.Encode(maybeNil, buffer)
	for index := 0; index < 2; index++ {
		hex.Encode(buffer[index:], buffer[index:])
		hex.Encode(buffer, buffer)
	}
}

func fields(holder struct{ buffer []byte }, pointer *struct{ buffer []byte }) {
	hex.Encode(holder.buffer, holder.buffer)
	hex.Encode(pointer.buffer, pointer.buffer)
	hex.Encode(shared, shared)
	hex.Encode(shared[1:], shared[1:])
}

func callShapes(buffer []byte, encoding *base64.Encoding) {
	defer hex.Encode(buffer, buffer)
	go ascii85.Encode(buffer, buffer)
	func() {
		base32.StdEncoding.Encode(buffer, buffer)
	}()
	encode := hex.Encode
	encode(buffer, buffer)
	method := base64.StdEncoding.Encode
	method(buffer, buffer)
	encoding.Encode(buffer, buffer)
	(*base64.Encoding).Encode(encoding, buffer, buffer)
	hex.Encode(
		buffer,
		buffer,
	)
	if hex.Encode(buffer, buffer) > 0 {
		return
	}
}

func generic[T any](value T, buffer []byte) T {
	hex.Encode(buffer, buffer)
	return value
}

func genericBuffer[B ~[]byte](buffer B) {
	hex.Encode(buffer, buffer)
	hex.Encode(buffer[1:], buffer[1:])
}

func instantiated() {
	generic(1, nil)
	genericBuffer([]byte{})
}
