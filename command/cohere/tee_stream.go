package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"

	"github.com/system-inc/cohere/internal/streams"
)

// teeStream copies a standard stream to its real destination and to a buffer.
type teeStream struct {
	real   *os.File
	writer *os.File
	buffer bytes.Buffer
	done   chan struct{}
}

// start points target at a pipe whose every line is copied to the real file and kept for the recording.
// buffered is the stream that writes to target (see package streams), shown first, so what it held from before
// the recording reaches the real file and stays out of it.
//
// Mutates: target the caller's stream variable is what every write goes through, so it is pointed at the pipe
func (stream *teeStream) start(target **os.File, buffered *bufio.Writer) error {
	reader, writer, pipeError := os.Pipe()
	if pipeError != nil {
		return pipeError
	}
	streams.Show(buffered)
	stream.real = *target
	stream.writer = writer
	stream.done = make(chan struct{})
	go func() {
		defer close(stream.done)
		// Ignored: the read end, closed once everything written to the pipe has been read from it.
		defer reader.Close()
		// Line by line, so a line tagged as describing this invocation reaches the terminal untagged
		// and never reaches the replay. The tag travels in the stream itself because the tee is a pipe:
		// noting the buffer's length at the moment of printing would read it before the pipe delivered.
		lines := bufio.NewReader(reader)
		for {
			line, readBytesError := lines.ReadBytes('\n')
			if len(line) > 0 {
				if rest, tagged := bytes.CutPrefix(line, invocationTag); tagged {
					stream.copyToReal(rest)
				} else if rest, tagged := bytes.CutPrefix(line, provenanceTag); tagged {
					// Kept tagged in the recording, so a replay knows to say where the line came from.
					stream.copyToReal(rest)
					stream.buffer.Write(line)
				} else {
					stream.copyToReal(line)
					stream.buffer.Write(line)
				}
			}
			if readBytesError != nil {
				return
			}
		}
	}()
	*target = writer
	return nil
}

// copyToReal copies one line to the real file. The copy runs on the tee's own goroutine, so a failure is
// reported to package streams rather than returned, and the exit code counts it there: a run whose output never
// reached the terminal must not exit as though it had.
func (stream *teeStream) copyToReal(line []byte) {
	if _, writeError := stream.real.Write(line); writeError != nil {
		streams.Report(fmt.Errorf("copying the run's output to %s: %w", stream.real.Name(), writeError))
	}
}

// stop restores the stream and waits for everything written so far to reach both destinations. buffered is
// the stream that writes to target, shown first, so what it held from during the recording is recorded.
//
// Mutates: target the caller's stream variable is pointed back at the real file start took it from
func (stream *teeStream) stop(target **os.File, buffered *bufio.Writer) {
	if stream.writer == nil {
		return
	}
	streams.Show(buffered)
	*target = stream.real
	// Ignored: closing the write end only tells the copy it has everything. A pipe's writes are not buffered, so
	// every one already reached the pipe, or failed and was kept by the stream that made it.
	_ = stream.writer.Close()
	<-stream.done
	stream.writer = nil
}
