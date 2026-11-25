package parity

import (
	"fmt"
	"net"
)

// elsewhereAddress is declared in this file and dialed in parity.go: found, naming that dial's line.
var elsewhereAddress = fmt.Sprintf("%s:%d", "localhost", 8080)

// A variable of parity.go dialed from this file: found, naming this file's line.
func dialFromElsewhere() {
	_, _ = net.Dial("tcp", packageAddress)
}
