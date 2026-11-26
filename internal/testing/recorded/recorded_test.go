package recorded

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gzipped is text gzipped, as `gzip -9n` writes an answers file.
func gzipped(test *testing.T, text string) []byte {
	test.Helper()
	var buffer bytes.Buffer
	writer, writerError := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if writerError != nil {
		test.Fatal(writerError)
	}
	if _, writeError := writer.Write([]byte(text)); writeError != nil {
		test.Fatal(writeError)
	}
	if closeError := writer.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	return buffer.Bytes()
}

func TestReadJsonReadsPlainAndGzippedFiles(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	type contents struct {
		Name string `json:"name"`
	}
	for name, data := range map[string][]byte{
		"plain.json":      []byte(`{"name": "x"}` + "\n"),
		"answers.json.gz": gzipped(test, `{"name": "x"}`+"\n"),
	} {
		path := filepath.Join(directory, name)
		if writeError := os.WriteFile(path, data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		read, readJsonError := ReadJson[contents](path)
		if readJsonError != nil || read.Name != "x" {
			test.Errorf("%s: read %+v, %v", name, read, readJsonError)
		}
	}
}

func TestReadJsonRefusesWhatDoesNotReadWhole(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	corrupt := gzipped(test, `{"name": "x"}`+"\n")
	// The CRC-32 sits in the trailer's first four bytes; one flipped bit there leaves the value decodable.
	corrupt[len(corrupt)-8] ^= 1
	for name, testCase := range map[string]struct {
		data []byte
		want string
	}{
		"two.json":        {[]byte(`{"name": "x"} {"name": "y"}`), "more than one JSON value"},
		"unknown.json":    {[]byte(`{"name": "x", "other": 1}`), "unknown field"},
		"corrupt.json.gz": {corrupt, "checksum"},
		"plain.json.gz":   {[]byte(`{"name": "x"}`), "gzip"},
	} {
		path := filepath.Join(directory, name)
		if writeError := os.WriteFile(path, testCase.data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		type contents struct {
			Name string `json:"name"`
		}
		if _, readJsonError := ReadJson[contents](path); readJsonError == nil ||
			!strings.Contains(readJsonError.Error(), testCase.want) {
			test.Errorf("%s: want an error naming %q, got %v", name, testCase.want, readJsonError)
		}
	}
}

func TestMatchPairsInputsWithTheirAnswers(test *testing.T) {
	test.Parallel()
	indexes, matchError := Match(
		[]Entry{{"a", "1"}, {"b", "2"}},
		[]Entry{{"b", "2"}, {"a", "1"}},
	)
	if matchError != nil || len(indexes) != 2 || indexes[0] != 1 || indexes[1] != 0 {
		test.Errorf("got %v, %v", indexes, matchError)
	}
}

func TestMatchRefusesAnswersThatAreNotTheInputs(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		inputs  []Entry
		answers []Entry
		want    string
	}{
		"unanswered":   {[]Entry{{"a", "1"}, {"b", "2"}}, []Entry{{"a", "1"}}, `"b" has no recorded answer`},
		"edited":       {[]Entry{{"a", "1"}}, []Entry{{"a", "2"}}, `"a" was recorded for another input, "2"`},
		"removed":      {[]Entry{{"a", "1"}}, []Entry{{"a", "1"}, {"b", "2"}}, `"b" is answered, and no input`},
		"input twice":  {[]Entry{{"a", "1"}, {"a", "1"}}, []Entry{{"a", "1"}}, `"a" is an input twice`},
		"answer twice": {[]Entry{{"a", "1"}}, []Entry{{"a", "1"}, {"a", "1"}}, `"a" is answered twice`},
	} {
		if _, matchError := Match(testCase.inputs, testCase.answers); matchError == nil ||
			!strings.Contains(matchError.Error(), testCase.want) {
			test.Errorf("%s: want an error naming %q, got %v", name, testCase.want, matchError)
		}
	}
}

func TestDigestIsTheSha256InHex(test *testing.T) {
	test.Parallel()
	// The SHA-256 of the empty input, as `shasum -a 256` prints it.
	if digest := Digest(nil); digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		test.Errorf("got %s", digest)
	}
}
