package unused_exports

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// A report that never reached the writer is an error, not a silent gap in what the reader was shown, and a
// writer that takes everything returns none. A pipe whose reader is closed refuses every write, so it stands
// in for a closed stdout.
func TestWriteReturnsTheWriteError(test *testing.T) {
	test.Parallel()
	report := &Report{
		Files:         []FileUnused{{FileName: "a.ts", Exports: 1}},
		FilesAnalyzed: 1,
	}
	reader, writer := io.Pipe()
	if closeError := reader.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	if writeError := Write(writer, report, false); !errors.Is(writeError, io.ErrClosedPipe) {
		test.Errorf("writing to a closed pipe returned %v, want %v", writeError, io.ErrClosedPipe)
	}
	var buffer bytes.Buffer
	if writeError := Write(&buffer, report, false); writeError != nil || !strings.Contains(buffer.String(), "a.ts") {
		test.Errorf("writing to a buffer returned %v and printed %q", writeError, buffer.String())
	}
}
