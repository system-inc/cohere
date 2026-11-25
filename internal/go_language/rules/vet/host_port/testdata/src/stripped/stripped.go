// Package stripped formats an address at package level and dials it in a body, which the strip takes away.
package stripped

import (
	"fmt"
	"net"
)

var host = "example.com"

var address = fmt.Sprintf("%s:%d", host, 80)

// connect dials address, and the strip takes its body.
func connect() { _, _ = net.Dial("tcp", address) }
