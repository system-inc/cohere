package timeline

import "sync/atomic"

// countedName is a build's name that counts how many times it was asked.
type countedName struct {
	asked *atomic.Int64
}

func (name countedName) String() string {
	name.asked.Add(1)
	return "windows/amd64"
}
