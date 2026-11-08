// Package parity holds the shapes staticcheck's representation builds a net.IP, its change to []byte or the call to
// bytes.Equal in more than one way, both the comparisons of two addresses and those of an address and other bytes.
package parity

import (
	"bytes"
	"net"
)

type address = net.IP

type definedAddress net.IP

type bytesOfAddress []byte

type holder struct {
	first  net.IP
	second net.IP
}

var (
	globalFirst  net.IP
	globalSecond = net.ParseIP("10.0.0.1")
)

var compared = bytes.Equal(globalFirst, globalSecond)

func returnsAddress() net.IP { return net.IPv4(127, 0, 0, 1) }

func addresses(first net.IP, second net.IP, aliased address, mask net.IPMask) {
	bytes.Equal(first, second)
	bytes.Equal(first, aliased)
	bytes.Equal(aliased, aliased)
	bytes.Equal([]byte(first), []byte(second))
	bytes.Equal(net.ParseIP("::1"), returnsAddress())
	bytes.Equal(net.IPv4zero, net.IPv6loopback)
	bytes.Equal(first.To4(), second.To16())
	bytes.Equal(first, first)
	_ = bytes.Equal(first, second) || bytes.Equal(second, first)
	if bytes.Equal(globalFirst, globalSecond) {
		return
	}
	holding := holder{first: first, second: second}
	bytes.Equal(holding.first, holding.second)
	pointer := &holding
	bytes.Equal(pointer.first, pointer.second)
	list := []net.IP{first, second}
	bytes.Equal(list[0], list[1])
	bytes.Equal(
		first,
		second,
	)
	bytes.Equal(mask, mask)
	bytes.Equal(first, mask)
}

func notAddresses(first net.IP, plain []byte, defined definedAddress, converted bytesOfAddress) {
	bytes.Equal(first, plain)
	bytes.Equal(plain, first)
	bytes.Equal(defined, defined)
	bytes.Equal(converted, converted)
	bytes.Equal(bytesOfAddress(first), bytesOfAddress(first))
	bytes.Equal(first, nil)
	bytes.Equal(nil, first)
	bytes.Equal([]byte("a"), []byte("b"))
	_ = first.Equal(first)
}

func phis(condition bool, first net.IP, second net.IP) {
	chosen := first
	if condition {
		chosen = second
	}
	bytes.Equal(chosen, second)
	var mixed []byte = first
	if condition {
		mixed = []byte{1}
	}
	bytes.Equal(mixed, second)
}

func callShapes(first net.IP, second net.IP) {
	defer bytes.Equal(first, second)
	go bytes.Equal(first, second)
	func() {
		bytes.Equal(first, second)
	}()
	equal := bytes.Equal
	equal(first, second)
}

func generic[T net.IP | []byte](first T, second T) {
	bytes.Equal(first, second)
}

func genericAddress[T ~[]byte](first T, second net.IP) {
	bytes.Equal(first, second)
	bytes.Equal(second, second)
}

func instantiated(first net.IP) {
	generic(first, first)
	genericAddress(first, first)
}
