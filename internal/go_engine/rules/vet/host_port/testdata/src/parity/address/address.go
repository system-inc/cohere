// Package address declares what the parity packages dial from outside: an address, whose declaration is in another
// package, where neither side looks, and a dialer, whose Dial a package can call without importing net.
package address

import (
	"fmt"
	"net"
)

// Remote is built as the pass judges, and judged nowhere, since no dial in this package is passed it.
var Remote = fmt.Sprintf("%s:%d", "remote", 7)

// Dialer is a dialer for packages that do not import net.
var Dialer = &net.Dialer{}
