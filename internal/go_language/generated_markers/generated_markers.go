package generated_markers

import (
	"bufio"
	"bytes"
	"io"
	"os"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
)

// generatedPrefix and generatedSuffix bound the marker `go generate`'s convention writes, and legacyCgoMarker is the
// line cgo wrote before Go 1.11, each as staticcheck spells them.
var (
	generatedPrefix = []byte("// Code generated ")
	generatedSuffix = []byte(" DO NOT EDIT.")
	legacyCgoMarker = []byte("// Created by cgo - DO NOT EDIT")
)

// ReadGeneratedMarkers is what generated.Analyzer's Run answers for pass: each file of the pass that holds a marker,
// by the name the file was parsed from, to the generator the marker names, as staticcheck's Run answers it. A file
// that cannot be opened is no generated file, as staticcheck reads it, so there is no error to answer.
func ReadGeneratedMarkers(pass *analysis.Pass) map[string]generated.Generator {
	generators := map[string]generated.Generator{}
	for _, file := range pass.Files {
		path := pass.Fset.PositionFor(file.Pos(), false).Filename
		opened, openError := os.Open(path)
		if openError != nil {
			continue
		}
		generator, marked := ScanGeneratedMarker(opened)
		// Ignored: the file was only read, and its judgment is made.
		_ = opened.Close()
		if marked {
			generators[path] = generator
		}
	}
	return generators
}

// ScanGeneratedMarker reads source a line at a time for the first marker, as staticcheck's isGenerated does, and
// answers its generator, or false at the end of source or on a read that fails. Each line is a slice of the reader's
// own buffer, which the next read writes over, and generatorOfLine, its last reader, keeps nothing of it: it answers a
// Generator, never the bytes.
func ScanGeneratedMarker(source io.Reader) (generated.Generator, bool) {
	reader := bufio.NewReader(source)
	for {
		line, readError := reader.ReadSlice('\n')
		if readError == bufio.ErrBufferFull {
			line, readError = longLineMarker(reader, line)
		}
		if readError != nil && readError != io.EOF {
			return generated.Unknown, false
		}
		line = bytes.TrimSuffix(line, []byte("\r\n"))
		line = bytes.TrimSuffix(line, []byte("\n"))
		if generator, marked := generatorOfLine(line); marked {
			return generator, true
		}
		if readError == io.EOF {
			return generated.Unknown, false
		}
	}
}

// longLineMarker finishes a line longer than the reader's buffer, whose first bufferful is opening. A line that does
// not open with the generated prefix can be no marker, the legacy cgo line being shorter than any buffer, so the rest
// of it is read past and it answers no line. One that does is gathered whole into a copy of its own, since the reads
// that bring the rest of it write over opening, which is a slice of the reader's buffer.
func longLineMarker(reader *bufio.Reader, opening []byte) ([]byte, error) {
	if !bytes.HasPrefix(opening, generatedPrefix) {
		for {
			_, readError := reader.ReadSlice('\n')
			if readError != bufio.ErrBufferFull {
				return nil, readError
			}
		}
	}
	line := bytes.Clone(opening)
	for {
		rest, readError := reader.ReadSlice('\n')
		line = append(line, rest...)
		if readError != bufio.ErrBufferFull {
			return line, readError
		}
	}
}

// generatorOfLine is the generator a line, its line ending trimmed, names when it is a marker, as staticcheck's
// isGenerated judges one: the generated prefix and suffix around a known generator's name, any other name being
// Unknown, or the legacy cgo line.
func generatorOfLine(line []byte) (generated.Generator, bool) {
	if bytes.HasPrefix(line, generatedPrefix) && bytes.HasSuffix(line, generatedSuffix) {
		// The prefix and suffix overlap in a line too short to hold both, which staticcheck marks with no name.
		if len(line)-len(generatedSuffix) < len(generatedPrefix) {
			return generated.Unknown, true
		}
		name := line[len(generatedPrefix) : len(line)-len(generatedSuffix)]
		// A conversion compared in a switch is not allocated.
		switch string(name) {
		case "by goyacc.":
			return generated.Goyacc, true
		case "by cmd/cgo;":
			return generated.Cgo, true
		case "by protoc-gen-go.":
			return generated.ProtocGenGo, true
		}
		if bytes.HasPrefix(name, []byte(`by "stringer `)) {
			return generated.Stringer, true
		}
		if bytes.HasPrefix(name, []byte("by goyacc ")) {
			return generated.Goyacc, true
		}
		return generated.Unknown, true
	}
	if bytes.Equal(line, legacyCgoMarker) {
		return generated.Cgo, true
	}
	return generated.Unknown, false
}
