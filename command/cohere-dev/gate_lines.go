package main

import (
	"sync"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// gateLines is where a gate's runs on its boxes say what they are doing: the terminal, each write shown at once and
// under the lock, so the two halves of a split gate, running at once, never write streams.Errors side by side, which a
// bufio.Writer can't take from two goroutines.
type gateLines struct {
	lock *sync.Mutex
}

// Write writes data to streams.Errors and shows it, holding the lock.
func (lines gateLines) Write(data []byte) (int, error) {
	lines.lock.Lock()
	defer lines.lock.Unlock()
	written, writeError := streams.Errors.Write(data)
	streams.Show(streams.Errors)
	return written, writeError
}
