package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Writer writes records one per line, and refuses the orderings the front door would refuse, so a stream
// cohere-go wrote out of order fails in cohere-go, with the record that broke it named, rather than at
// the front door as "cohere-go is broken".
//
// A Writer is not safe for concurrent use. The pipeline writes in order, and the contract's order is the
// pipeline's.
type Writer struct {
	output            io.Writer
	provenanceWritten bool
	summaryWritten    bool

	// line is the record being written, encoded by encoder, which writes into it, and emptied before the next. One
	// buffer serves every record, since a run writes thousands of findings and each would otherwise encode into a
	// buffer of its own and be copied into a line of its own. Its last reader is output's Write, which io.Writer
	// forbids to keep it: cohere-go's stdout copies it out to the pipe, and a test's bytes.Buffer into its own bytes.
	line    bytes.Buffer
	encoder *json.Encoder
}

// NewWriter is a Writer onto output, which is cohere-go's stdout outside a test.
func NewWriter(output io.Writer) *Writer {
	writer := &Writer{output: output}
	writer.encoder = json.NewEncoder(&writer.line)
	// A message that says `a <- b` should read that way in the stream too (encodeWithoutHtmlEscaping).
	writer.encoder.SetEscapeHTML(false)
	return writer
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

	line, encodeError := writer.encodeLine(record)
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

// encodeLine is a record as one line of JSON ending in a newline, with its kind as the first field, in writer.line:
// the line is good until the next record is encoded, which empties the buffer under it.
func (writer *Writer) encodeLine(record Record) ([]byte, error) {
	writer.line.Reset()
	writer.line.WriteString(`{"kind":`)
	if encodeError := writer.encoder.Encode(record.kind()); encodeError != nil {
		return nil, encodeError
	}
	// The encoder ends every value with a newline, which the kind is not the end of.
	writer.line.Truncate(writer.line.Len() - 1)
	fieldsStart := writer.line.Len()
	if encodeError := writer.encoder.Encode(record); encodeError != nil {
		return nil, encodeError
	}
	line := writer.line.Bytes()
	fields := line[fieldsStart : len(line)-1]
	// Every record type is a struct with fields, so fields is an object with at least one member, and its opening
	// brace becomes the comma between the kind and its first member.
	if len(fields) < 2 || fields[0] != '{' || string(fields) == "{}" {
		return nil, fmt.Errorf("it encodes as %s, which is not an object with fields", fields)
	}
	line[fieldsStart] = ','
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
