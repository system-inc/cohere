// Package indirect calls (*net.Dialer).Dial without importing net, on a dialer another package declares: found, since
// the original's index learns of net from the method it uses.
package indirect

import (
	"fmt"

	"parity/address"
)

func indirect(host string, port int) {
	_, _ = address.Dialer.Dial("tcp", fmt.Sprintf("%s:%d", host, port))
	local := fmt.Sprintf("%s:%s", host, "80")
	_, _ = address.Dialer.Dial("tcp", local)
}
