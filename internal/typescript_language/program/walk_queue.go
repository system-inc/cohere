package program

import "sync"

// walkQueue is one worker's group of files. The owner takes from the front, and a worker whose own group
// is done takes from the back of the busiest group, walking what it takes on its own checker.
//
// Any checker can type any file of the program; ownership decides which checker reports a file's type
// diagnostics, not which may answer questions about it. What must not happen is one rule mixing answers
// from two checkers, and that cannot: every query for a file comes from the one checker that file is
// walked on. The cost is that the taking checker resolves the stolen file's imports for itself, on a core
// that would otherwise be idle. TestEveryFileWalkedOnAForeignCheckerFindsTheSame holds the findings and the
// type diagnostics identical whichever checker walks a file.
type walkQueue struct {
	mutex       sync.Mutex
	indices     []int
	front, back int
}

// next is the owner's next file.
func (queue *walkQueue) next() (int, bool) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.front >= queue.back {
		return 0, false
	}
	queue.front++
	return queue.indices[queue.front-1], true
}

// steal is a file from the far end, for another worker.
func (queue *walkQueue) steal() (int, bool) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.front >= queue.back {
		return 0, false
	}
	queue.back--
	return queue.indices[queue.back], true
}

func (queue *walkQueue) remaining() int {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.back - queue.front
}
