package doc

import "unsafe"

// printResult is upstream's PrintResult without cursor tracking.
//
// The settled and unsettled split is kept to read against upstream, not because output depends on it.
// Upstream needs it for cursor positions. For text it is unobservable: everything settled was settled
// by a trim, so it ends in a non-space, and a trim reaching past it would remove nothing. A mutation
// that stopped settling survived 5,000 differential docs, and that is why this comment says so.
//
// Both halves are one buffer, with the settled length marked, so settling copies nothing and the unsettled
// half keeps its memory from line to line: two builders, one reset on every settle, regrew it each line,
// and were 2.7M of the JavaScript printer's allocations on ahra (#fyw36kf).
type printResult struct {
	buffer  []byte
	settled int
}

func (result *printResult) write(text string) { result.buffer = append(result.buffer, text...) }

func (result *printResult) settle() { result.settled = len(result.buffer) }

// trim is upstream's trimIndentation over the unsettled half: its trailing spaces and tabs, and their count.
func (result *printResult) trim() int {
	count := 0
	for index := len(result.buffer) - 1; index >= result.settled; index-- {
		if result.buffer[index] != ' ' && result.buffer[index] != '\t' {
			break
		}
		count++
	}
	result.buffer = result.buffer[:len(result.buffer)-count]
	result.settle()
	return count
}

// finish is the printed text. It hands back the buffer itself rather than a copy: nothing writes to a
// printResult once it is finished, so the bytes can't change under the string.
func (result *printResult) finish() string {
	result.settle()
	if len(result.buffer) == 0 {
		return ""
	}
	return unsafe.String(&result.buffer[0], len(result.buffer))
}
