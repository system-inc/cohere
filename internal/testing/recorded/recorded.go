// Package recorded reads the upstream answers a test compares against, recorded once beside it, so the test
// runs no process and reads only this repository (Kirk, Nov 25, #5wj23dd).
//
// A recorded comparison keeps two files in its package's testdata: the inputs, plain JSON that the test and
// its recorder both read, and the answers, which the recorder prints and which are gzipped when they pass
// 50 KB. Each answer carries its input's name and the text it was recorded for (or a digest of a vendored
// file), so an edited input fails as an answer recorded for another text rather than comparing against a
// stale one. The recorders are node scripts attached to the task that added each comparison; none is
// committed, and none runs in a test.
package recorded

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadJson decodes the one JSON value in the file at path as Contents, unzipping it first when the name ends
// in .gz, and refuses a field Contents does not declare, so a misspelled key fails rather than reading as
// absent. It refuses a file with anything after that value, and it reads a gzipped file to its end, since
// the gzip trailer's checksum is checked only there: a corrupt file whose first value happens to decode
// still fails.
func ReadJson[Contents any](path string) (Contents, error) {
	var decoded Contents
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return decoded, readError
	}
	var reader io.Reader = bytes.NewReader(contents)
	if strings.HasSuffix(path, ".gz") {
		unzipped, gzipError := gzip.NewReader(reader)
		if gzipError != nil {
			return decoded, fmt.Errorf("%s: %w", path, gzipError)
		}
		reader = unzipped
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&decoded); decodeError != nil {
		return decoded, fmt.Errorf("%s: %w", path, decodeError)
	}
	var trailing json.RawMessage
	switch trailingError := decoder.Decode(&trailing); {
	case trailingError == nil:
		return decoded, fmt.Errorf("%s holds more than one JSON value", path)
	case !errors.Is(trailingError, io.EOF):
		return decoded, fmt.Errorf("%s: %w", path, trailingError)
	}
	return decoded, nil
}

// Entry is one input or one answer as Match pairs them: its name, and what it was recorded for, the input's
// text or Digest of a vendored file.
type Entry struct {
	Name  string
	Input string
}

// Match pairs every input with its answer by name: the result holds, for each input in order, the index of
// its answer. Every input must have exactly one answer recorded for the same input, and every answer an
// input, so a renamed, edited, added or removed input fails until the answers are recorded again.
func Match(inputs []Entry, answers []Entry) ([]int, error) {
	answerIndex := make(map[string]int, len(answers))
	for index, answer := range answers {
		if _, repeated := answerIndex[answer.Name]; repeated {
			return nil, fmt.Errorf("%q is answered twice", answer.Name)
		}
		answerIndex[answer.Name] = index
	}
	inputNames := make(map[string]bool, len(inputs))
	indexes := make([]int, len(inputs))
	for position, input := range inputs {
		if inputNames[input.Name] {
			return nil, fmt.Errorf("%q is an input twice", input.Name)
		}
		inputNames[input.Name] = true
		index, answered := answerIndex[input.Name]
		if !answered {
			return nil, fmt.Errorf("%q has no recorded answer", input.Name)
		}
		if answers[index].Input != input.Input {
			return nil, fmt.Errorf("%q was recorded for another input, %q", input.Name, answers[index].Input)
		}
		indexes[position] = index
	}
	for _, answer := range answers {
		if !inputNames[answer.Name] {
			return nil, fmt.Errorf("%q is answered, and no input has that name any more", answer.Name)
		}
	}
	return indexes, nil
}

// Digest is what an answer for a vendored file records instead of its text: the file's SHA-256, in hex,
// so a changed file fails Match without the answers holding a second copy of it.
func Digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
