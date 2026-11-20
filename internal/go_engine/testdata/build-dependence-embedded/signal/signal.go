// Package signal embeds Marker, whose method set windows changes in a file only it compiles, so Notice's method set
// differs on windows, which only that file's syntax says.
package signal

import "example.com/embedded/marker"

// Notice carries a Marker.
type Notice struct {
	marker.Marker
}
