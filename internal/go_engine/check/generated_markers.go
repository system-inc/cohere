package check

import (
	"bufio"
	"bytes"
	"io"
	"os"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
)

// staticcheck's generated.Analyzer judges every file of a package it runs on by reading it line by line for a
// "Code generated" marker, and its reader's ReadBytes copies each line into a slice of its own: one allocation for
// every line of every file checked, only to compare it and drop it. readGeneratedMarkers judges the same lines in
// place, in the reader's own buffer, and replaces the analyzer's Run as the package initializes, in place as wrapRuns
// wraps it and for the same reason: every pass reads what it requires keyed by the analyzer's own pointer, so a copy
// standing in for it would be read by nobody. The judgment is staticcheck's, line for line (scanGeneratedMarker), at
// the version go.mod links, v0.8.1; its result is the same type, so every reader of pass.ResultOf[generated.Analyzer]
// reads what it read before.

// staticcheckGeneratedRun is generated.Analyzer's own Run, taken before generatedMarkersReplaced replaces it, so a test
// holds the scanner to staticcheck's judgment on the same files. It is declared before generatedMarkersReplaced, which
// the language initializes after it.
var staticcheckGeneratedRun = generated.Analyzer.Run

// generatedMarkersReplaced is whether generated.Analyzer runs readGeneratedMarkers, which it does from the package's
// initialization, before any pass could run it and before wrapRuns wraps it.
var generatedMarkersReplaced = replaceGeneratedRun()

// replaceGeneratedRun makes readGeneratedMarkers generated.Analyzer's Run. Only the package's initialization calls it.
func replaceGeneratedRun() bool {
	generated.Analyzer.Run = readGeneratedMarkers
	return true
}

// generatedPrefix and generatedSuffix bound the marker `go generate`'s convention writes, and legacyCgoMarker is the
// line cgo wrote before Go 1.11, each as staticcheck spells them.
var (
	generatedPrefix = []byte("// Code generated ")
	generatedSuffix = []byte(" DO NOT EDIT.")
	legacyCgoMarker = []byte("// Created by cgo - DO NOT EDIT")
)

// readGeneratedMarkers is generated.Analyzer's Run: each file of the pass that holds a marker, by the name the file
// was parsed from, to the generator the marker names, as staticcheck's Run answers it. A file that cannot be opened
// is no generated file, as staticcheck reads it.
func readGeneratedMarkers(pass *analysis.Pass) (any, error) {
	generators := map[string]generated.Generator{}
	for _, file := range pass.Files {
		path := pass.Fset.PositionFor(file.Pos(), false).Filename
		opened, openError := os.Open(path)
		if openError != nil {
			continue
		}
		generator, marked := scanGeneratedMarker(opened)
		// Ignored: the file was only read, and its judgment is made.
		_ = opened.Close()
		if marked {
			generators[path] = generator
		}
	}
	return generators, nil
}

// scanGeneratedMarker reads source a line at a time for the first marker, as staticcheck's isGenerated does, and
// answers its generator, or false at the end of source or on a read that fails. Each line is a slice of the reader's
// own buffer, which the next read writes over, and generatorOfLine, its last reader, keeps nothing of it: it answers a
// Generator, never the bytes.
func scanGeneratedMarker(source io.Reader) (generated.Generator, bool) {
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
