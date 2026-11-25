// Package dotted dot-imports fmt and net, so Sprintf and Dial are called by their bare names, which name the same
// functions: found.
package dotted

import (
	. "fmt"
	. "net"
)

func dotted(host string, port int) {
	_, _ = Dial("tcp", Sprintf("%s:%d", host, port))
	address := Sprintf("%s:%s", host, "80")
	_, _ = DialTimeout("tcp", address, 0)
	var dialer Dialer
	_, _ = dialer.Dial("tcp", Sprintf("%s:%d", host, 443))
}
