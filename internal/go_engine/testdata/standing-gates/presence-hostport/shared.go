package gate

import "fmt"

// address is hostport's candidate, declared in the file both builds compile.
var address = fmt.Sprintf("%s:%d", "localhost", 80)

// Dial names address, so hostport reads it, and the row names the body whether or not it dials (hostPortNamed). It
// holds nothing of net's: the reach judges net.Conn's name and underlying changed between a linux host and windows,
// which would reach Dial by its signature or a value's type, and then whether it stands would rest on the host.
func Dial() string { return "tcp " + address }

// Port names no candidate.
func Port() int { return 80 }
