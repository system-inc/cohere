// Package gate leaks a ticker, which SA1015 reports unless a file of the package imports cobra, a command's.
package gate

import "time"

// Target leaks a ticker.
func Target() { _ = time.Tick(time.Second) }
