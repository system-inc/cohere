package gate

import (
	"fmt"
	"net"
)

// address is hostport's candidate, declared in the file both builds compile.
var address = fmt.Sprintf("%s:%d", "localhost", 80)

// Dial names address, so hostport reads it.
func Dial() (net.Conn, error) { return net.Dial("tcp", address) }

// Port names no candidate.
func Port() int { return 80 }
