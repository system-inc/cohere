package streams

import "os"

// following writes to whichever file a standard stream's variable names at the moment of the write, not the
// file it named when the program started.
//
// cohere's run cache records a run by pointing os.Stdout and os.Stderr at pipes it reads, and points them back
// when the run ends (see command/cohere's teeStream). A writer bound to the file at startup would write around
// that recording, and a replay of the run would print nothing. Following the variable keeps one buffered
// writer per stream for the life of the process, with the swap beneath it, as long as the buffer is shown
// before each swap so what was written on one side of it lands there.
type following struct {
	file **os.File
}

func (stream following) Write(data []byte) (int, error) {
	return (*stream.file).Write(data)
}
