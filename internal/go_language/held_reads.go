package go_language

import (
	"errors"
	"fmt"
	"sync"
)

// heldReads are the description's reads of the census's files that are ours, each in a slot of its own, which
// readOurs fills and project.BuildsFor reads (#ghb3rcn): BuildsFor takes each file's bytes once its read is done, as
// the reads go on, so no file of the census is opened twice and the builds never wait for every read to end. A slot
// is closed once, by its read or by abandon, so a reader never waits on a read that will never come.
type heldReads struct {
	// files are the census's files that are ours, in its order, and indexes each one's slot by its path.
	files   []string
	indexes map[string]int

	// slots are each file's read, by its index in files.
	slots []heldRead
}

// heldRead is one file's read: its bytes or why they could not be read, written once before done is closed and
// read only after.
type heldRead struct {
	closing   sync.Once
	done      chan struct{}
	contents  []byte
	readError error
}

// errorNeverRead is why a file whose read the description never reached has no bytes: it ended first, refused or
// failed, and a run that ended so reads no builds.
var errorNeverRead = errors.New("the description ended before it read the file")

// newHeldReads is a slot for each of files, none of them read yet.
func newHeldReads(files []string) *heldReads {
	reads := &heldReads{files: files, indexes: make(map[string]int, len(files)), slots: make([]heldRead, len(files))}
	for index, file := range files {
		reads.indexes[file] = index
		reads.slots[index].done = make(chan struct{})
	}
	return reads
}

// hold keeps the read of the file at index and lets its reader go, unless its slot is closed already.
func (reads *heldReads) hold(index int, contents []byte, readError error) {
	slot := &reads.slots[index]
	slot.closing.Do(func() {
		slot.contents, slot.readError = contents, readError
		close(slot.done)
	})
}

// abandon closes every slot not yet held as never read, so a reader waiting on one returns: deferred by the
// description, it runs on every way out of it. Nothing returns between the slots' making and readOurs filling every
// one, so in the description it guards a panic alone.
func (reads *heldReads) abandon() {
	for index := range reads.slots {
		reads.hold(index, nil, errorNeverRead)
	}
}

// read is file's bytes as the description read them, once they are, and why they could not be read otherwise: the
// reader project.BuildsFor is handed.
func (reads *heldReads) read(file string) ([]byte, error) {
	index, ours := reads.indexes[file]
	if !ours {
		// BuildsFor asks only for the census's files that are ours, so this is a caller's mistake, named.
		return nil, fmt.Errorf("%s is no file of ours the description reads", file)
	}
	slot := &reads.slots[index]
	<-slot.done
	return slot.contents, slot.readError
}
