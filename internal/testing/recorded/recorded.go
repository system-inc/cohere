// Package recorded reads the upstream answers a test compares against, recorded once beside it, so the test
// runs no process and reads only this repository (Kirk, Nov 25, #5wj23dd).
//
// A recorded comparison keeps two files in its package's testdata: the inputs, plain JSON that the test and
// its recorder both read, and the answers, plain JSON that the recorder prints. Neither is ever compressed:
// cohere is public, and its landing gate turns away compressed fixtures on a public repository, since what
// they hold cannot be read in review. A set past 50 KB is split into numbered parts (Parts), each saying
// which part it is of how many. Where a test only compares an answer for equality, the answer is kept as its
// Fingerprint, since it can never be recorded again. Each answer carries its input's name and the text it was
// recorded for (or a digest of a long or vendored text), so an edited input fails as an answer recorded for
// another text rather than comparing against a stale one. The recorders are node scripts attached to the task
// that added each comparison; none is committed, and none runs in a test.
package recorded

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/json_value"
)

// ReadJson decodes the one JSON value in the file at path as Contents, and refuses a field Contents does not
// declare, so a misspelled key fails rather than reading as absent. It refuses a file with anything after that
// value, and a path ending in .gz before reading it, since compressed fixtures never land on a public
// repository.
func ReadJson[Contents any](path string) (Contents, error) {
	var decoded Contents
	if strings.HasSuffix(path, ".gz") {
		return decoded, fmt.Errorf(
			"%s is compressed, and compressed fixtures never land on a public repository: record it as plain "+
				"JSON, split into parts under 50 KB",
			path,
		)
	}
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return decoded, readError
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
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

// Fingerprint is what a test may store of an answer in place of its text: the first 16 hex digits of the
// text's SHA-256, a colon, and its length in bytes, as 3f2a9c0b1d4e5f67:1204. A recorded answer can never
// be recorded again, since the fork's bundles that answered are gone, so a mismatch is always a regression in
// our own code, found by bisecting our history rather than by reading the answer. The answer's text adds
// nothing to that but bytes, and the length says at a glance how far apart two outputs are.
func Fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%s:%d", hex.EncodeToString(sum[:8]), len(text))
}

// CanonicalJson is a value as an answer's fingerprint was taken of it: encoded, decoded untyped and encoded
// again by encoding/json, so object keys are sorted and every number reads as a float64 (1 and 1.0 are one
// number, as they are in JavaScript), and two values are one string exactly when they hold the same keys and
// values. The recorded answers were written through that untyped decode and encode. This reaches the same
// bytes through json_value's typed tree, every number put through a float64 as the untyped decode does.
func CanonicalJson[Value any](value Value) (string, error) {
	encoded, encodeError := json.Marshal(value)
	if encodeError != nil {
		return "", encodeError
	}
	parsed, parseError := json_value.Parse(encoded)
	if parseError != nil {
		return "", parseError
	}
	numbersAsFloats, convertError := withNumbersAsFloats(parsed)
	if convertError != nil {
		return "", convertError
	}
	canonical, encodeAgainError := json.Marshal(numbersAsFloats)
	if encodeAgainError != nil {
		return "", encodeAgainError
	}
	return string(canonical), nil
}

// withNumbersAsFloats is value with every number written as encoding/json writes the float64 it parses to,
// which is how an untyped decode holds a number.
func withNumbersAsFloats(value json_value.Value) (json_value.Value, error) {
	switch typed := value.(type) {
	case json_value.Number:
		float, parseError := strconv.ParseFloat(string(typed), 64)
		if parseError != nil {
			return nil, parseError
		}
		written, encodeError := json.Marshal(float)
		if encodeError != nil {
			return nil, encodeError
		}
		return json_value.Number(written), nil
	case json_value.Object:
		converted := make(json_value.Object, len(typed))
		for key, member := range typed {
			convertedMember, convertError := withNumbersAsFloats(member)
			if convertError != nil {
				return nil, convertError
			}
			converted[key] = convertedMember
		}
		return converted, nil
	case json_value.Array:
		converted := make(json_value.Array, len(typed))
		for index, element := range typed {
			convertedElement, convertError := withNumbersAsFloats(element)
			if convertError != nil {
				return nil, convertError
			}
			converted[index] = convertedElement
		}
		return converted, nil
	}
	return value, nil
}
