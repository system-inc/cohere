package load

import "sync"

// Rewritten is the bytes the load parsed a file from where they are neither the disk's nor the held bytes: each cgo
// output whose `//line` directives it pointed at its package's directory (see rewriteCgoLines), by the path the load
// names it. A reader that must read what a file's syntax was parsed from, as a rule's pass.ReadFile must, since every
// offset the syntax holds is into those bytes, reads a file here before anywhere else. A file is added before its
// parse is published, so it is here once its package is ready for analysis (see Overlap.Ready); the lock is for a
// reader of one package while the load still adds another's. The bytes are shared, so a reader must not modify them.
type Rewritten struct {
	lock     sync.Mutex
	contents map[string][]byte
}

// newRewritten is a Rewritten holding nothing yet.
func newRewritten() *Rewritten {
	return &Rewritten{contents: map[string][]byte{}}
}

// Lookup is the bytes the load parsed file from when it rewrote them, and false when it did not, or rewritten is nil.
func (rewritten *Rewritten) Lookup(file string) ([]byte, bool) {
	if rewritten == nil {
		return nil, false
	}
	rewritten.lock.Lock()
	defer rewritten.lock.Unlock()
	contents, isRewritten := rewritten.contents[file]
	return contents, isRewritten
}

// add keeps contents as the bytes file was parsed from.
func (rewritten *Rewritten) add(file string, contents []byte) {
	rewritten.lock.Lock()
	defer rewritten.lock.Unlock()
	rewritten.contents[file] = contents
}
