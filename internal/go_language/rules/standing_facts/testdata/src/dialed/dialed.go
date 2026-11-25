// Package dialed is host_port's stripped fixture in presence's form: an address formatted at package level and dialed
// in a body, connect.
package dialed

import (
	"fmt"
	"net"
)

var host = "example.com"

var address = fmt.Sprintf("%s:%d", host, 80)

// connect dials address, and the strip takes its body.
func connect() { _, _ = net.Dial("tcp", address) }
