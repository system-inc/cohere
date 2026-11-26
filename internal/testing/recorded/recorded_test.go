package recorded

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strconv"
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

func TestAVendoredInputIsMatchedByItsDigest(test *testing.T) {
	test.Parallel()
	text := "a: b\n"
	own := Inputs{Inputs: []Input{{Name: "a", Text: text}}}
	vendored := Inputs{Vendored: true, Inputs: []Input{{Name: "a", Text: text}}}
	byText := RecordedFor{Name: "a", Text: text}
	byDigest := RecordedFor{Name: "a", Sha256: Digest([]byte(text))}
	for name, testCase := range map[string]struct {
		inputs  Inputs
		answer  RecordedFor
		matches bool
	}{
		"own input, answered by its text":        {own, byText, true},
		"vendored input, answered by its digest": {vendored, byDigest, true},
		"vendored input, answered by its text":   {vendored, byText, false},
		"own input, answered by a digest":        {own, byDigest, false},
		"vendored input, edited": {
			Inputs{Vendored: true, Inputs: []Input{{Name: "a", Text: "a: c\n"}}},
			byDigest,
			false,
		},
	} {
		_, matchError := Match(testCase.inputs.Entries(), []Entry{testCase.answer.Entry()})
		if (matchError == nil) != testCase.matches {
			test.Errorf("%s: want a match %v, got %v", name, testCase.matches, matchError)
		}
	}
}

func TestAnAnswerFileReadsWithItsRecordedForFields(test *testing.T) {
	test.Parallel()
	type answer struct {
		RecordedFor
		Output string `json:"output"`
	}
	path := filepath.Join(test.TempDir(), "answers.json.gz")
	text := `{"source": "s", "recorded": "2026-11-25", "answers": [` +
		`{"name": "a", "text": "x", "output": "1"}, {"name": "b", "sha256": "d", "output": "2"}]}`
	if writeError := os.WriteFile(path, gzipped(test, text), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	answers, readJsonError := ReadJson[Answers[answer]](path)
	if readJsonError != nil {
		test.Fatal(readJsonError)
	}
	if len(answers.Answers) != 2 ||
		answers.Answers[0].Entry() != (Entry{"a", "x"}) ||
		answers.Answers[1].Entry() != (Entry{"b", "d"}) ||
		answers.Answers[1].Output != "2" {
		test.Errorf("read %+v", answers)
	}
}

func TestReadInputsKeysEachFileByItsOwnVendoredFlag(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	own := filepath.Join(directory, "own.json")
	vendored := filepath.Join(directory, "vendored.json")
	empty := filepath.Join(directory, "empty.json")
	for path, text := range map[string]string{
		own:      `{"about": "ours", "inputs": [{"name": "a", "text": "x"}]}`,
		vendored: `{"about": "theirs", "vendored": true, "inputs": [{"name": "b", "text": "y", "about": "a case"}]}`,
		empty:    `{"about": "none", "inputs": []}`,
	} {
		if writeError := os.WriteFile(path, []byte(text), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	inputs, entries, readInputsError := ReadInputs(own, vendored)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	if len(inputs) != 2 || inputs[1].Text != "y" || inputs[1].About != "a case" ||
		entries[0] != (Entry{"a", "x"}) || entries[1] != (Entry{"b", Digest([]byte("y"))}) {
		test.Errorf("read %+v, %+v", inputs, entries)
	}
	_, _, emptyError := ReadInputs(own, empty)
	if emptyError == nil || !strings.Contains(emptyError.Error(), "no inputs") {
		test.Errorf("an empty inputs file read as %v", emptyError)
	}
}

func TestPartsAreReadInNumberOrderAndNoneIsMissing(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	write := func(name string) {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for part := 1; part <= 10; part++ {
		write("cases_" + strconv.Itoa(part) + ".json")
	}
	parts, partsError := Parts(directory, "cases_")
	if partsError != nil || len(parts) != 10 || filepath.Base(parts[1]) != "cases_2.json" ||
		filepath.Base(parts[9]) != "cases_10.json" {
		test.Fatalf("got %v, %v", parts, partsError)
	}
	write("cases_12.json")
	_, gapError := Parts(directory, "cases_")
	if gapError == nil || !strings.Contains(gapError.Error(), "cases_11.json") {
		test.Errorf("a gap at 11 read as %v", gapError)
	}
	if _, noneError := Parts(directory, "dumps_"); noneError == nil {
		test.Error("no parts at all read as no error")
	}
}
