package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Writer writes records one per line, and refuses the orderings the front door would refuse, so a stream
// the engine wrote out of order fails in the engine, with the record that broke it named, rather than at
// the front door as "the engine is broken".
//
// A Writer is not safe for concurrent use. The pipeline writes in order, and the contract's order is the
// pipeline's.
type Writer struct {
	output            io.Writer
	provenanceWritten bool
	summaryWritten    bool
}

// NewWriter is a Writer onto output, which is the engine's stdout outside a test.
func NewWriter(output io.Writer) *Writer {
	return &Writer{output: output}
}

// Write writes one record as one line, with `kind` first, as the fixtures in swift/Contract/ write it.
//
// It refuses a record before provenance, a second provenance, and any record after the summary, each
// of which the front door refuses too. A refused record writes nothing. The line goes out in one call to
// the underlying writer, so a reader never sees half a record.
func (writer *Writer) Write(record Record) error {
	if writer.summaryWritten {
		return fmt.Errorf("a %q record after the summary, which is always last", record.kind())
	}
	if !writer.provenanceWritten && record.kind() != KindProvenance {
		return fmt.Errorf("a %q record before provenance, which is always first", record.kind())
	}
	if writer.provenanceWritten && record.kind() == KindProvenance {
		return fmt.Errorf("a second provenance record")
	}

	line, encodeError := encodeLine(record)
	if encodeError != nil {
		return fmt.Errorf("a %q record that does not encode: %w", record.kind(), encodeError)
	}
	if _, writeError := writer.output.Write(line); writeError != nil {
		return fmt.Errorf("writing a %q record: %w", record.kind(), writeError)
	}

	switch record.kind() {
	case KindProvenance:
		writer.provenanceWritten = true
	case KindSummary:
		writer.summaryWritten = true
	}
	return nil
}

// encodeLine is a record as one line of JSON ending in a newline, with its kind as the first field.
func encodeLine(record Record) ([]byte, error) {
	fields, encodeError := encodeWithoutHtmlEscaping(record)
	if encodeError != nil {
		return nil, encodeError
	}
	kind, encodeError := encodeWithoutHtmlEscaping(record.kind())
	if encodeError != nil {
		return nil, encodeError
	}
	// Every record type is a struct with fields, so fields is an object with at least one member, and
	// the kind goes in after its opening brace.
	if len(fields) < 2 || fields[0] != '{' || string(fields) == "{}" {
		return nil, fmt.Errorf("it encodes as %s, which is not an object with fields", fields)
	}
	line := make([]byte, 0, len(fields)+len(kind)+12)
	line = append(line, `{"kind":`...)
	line = append(line, kind...)
	line = append(line, ',')
	line = append(line, fields[1:]...)
	line = append(line, '\n')
	return line, nil
}

// encodeWithoutHtmlEscaping is json.Marshal without its escaping of <, > and &, which is for JSON
// embedded in HTML. A message that says `a <- b` should read that way in the stream too. It takes a value of
// any type, as json.Marshal does.
func encodeWithoutHtmlEscaping[Value any](value Value) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encodeError := encoder.Encode(value); encodeError != nil {
		return nil, encodeError
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
