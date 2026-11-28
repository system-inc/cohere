package recorded

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
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

func TestReadJsonReadsAPlainFile(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	type contents struct {
		Name string `json:"name"`
	}
	for name, data := range map[string][]byte{
		"plain.json": []byte(`{"name": "x"}` + "\n"),
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
	for name, testCase := range map[string]struct {
		data []byte
		want string
	}{
		"two.json":     {[]byte(`{"name": "x"} {"name": "y"}`), "more than one JSON value"},
		"unknown.json": {[]byte(`{"name": "x", "other": 1}`), "unknown field"},
		// A well-formed gzip of a well-formed value is refused by its name, before it is read.
		"answers.json.gz": {gzipped(test, `{"name": "x"}`+"\n"), "never land on a public repository"},
		"plain.json.gz":   {[]byte(`{"name": "x"}`), "never land on a public repository"},
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
	path := filepath.Join(test.TempDir(), "answers.json")
	text := `{"source": "s", "recorded": "2026-11-25", "answers": [` +
		`{"name": "a", "text": "x", "output": "1"}, {"name": "b", "sha256": "d", "output": "2"}]}`
	if writeError := os.WriteFile(path, []byte(text), 0o644); writeError != nil {
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

// writePart writes directory's name, saying it is part of parts.
func writePart(test *testing.T, directory string, name string, part int, parts int) {
	test.Helper()
	text := fmt.Sprintf(`{"about": "a part", "part": %d, "parts": %d, "inputs": []}`, part, parts)
	if writeError := os.WriteFile(filepath.Join(directory, name), []byte(text), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// writeParts writes count parts of directory named prefix, each numbered as Parts reads them.
func writeParts(test *testing.T, directory string, prefix string, count int) {
	test.Helper()
	for part := 1; part <= count; part++ {
		writePart(test, directory, prefix+strconv.Itoa(part)+".json", part, count)
	}
}

func TestPartsAreReadInNumberOrderAndNoneIsMissing(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeParts(test, directory, "cases_", 10)
	parts, partsError := Parts(directory, "cases_")
	if partsError != nil || len(parts) != 10 || filepath.Base(parts[1]) != "cases_2.json" ||
		filepath.Base(parts[9]) != "cases_10.json" {
		test.Fatalf("got %v, %v", parts, partsError)
	}
	writePart(test, directory, "cases_12.json", 12, 12)
	_, gapError := Parts(directory, "cases_")
	if gapError == nil || !strings.Contains(gapError.Error(), "cases_11.json") {
		test.Errorf("a gap at 11 read as %v", gapError)
	}
	if _, noneError := Parts(directory, "dumps_"); noneError == nil {
		test.Error("no parts at all read as no error")
	}
}

// TestAnAnswersSetSplitsAsAnInputsSetDoes: the answers to a split inputs set are split and read the same way.
func TestAnAnswersSetSplitsAsAnInputsSetDoes(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	for part := 1; part <= 2; part++ {
		text := fmt.Sprintf(`{"source": "s", "recorded": "2026-11-28", "part": %d, "parts": 2, "answers": []}`, part)
		path := filepath.Join(directory, "suite_answers_"+strconv.Itoa(part)+".json")
		if writeError := os.WriteFile(path, []byte(text), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	parts, partsError := Parts(directory, "suite_answers_")
	if partsError != nil || len(parts) != 2 || filepath.Base(parts[1]) != "suite_answers_2.json" {
		test.Fatalf("got %v, %v", parts, partsError)
	}
	for index, path := range parts {
		answers, readJsonError := ReadJson[Answers[RecordedFor]](path)
		if readJsonError != nil || answers.Part != index+1 || answers.Parts != 2 {
			test.Errorf("%s read as %+v, %v", path, answers, readJsonError)
		}
	}
}

// TestPartsThatDoNotAddUpAreRefused: every part says which it is of how many, so a set that lost its last
// part, whose parts disagree on their count, or whose part says another number, fails; and so does a set
// with a compressed part.
func TestPartsThatDoNotAddUpAreRefused(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		plant func(directory string)
		want  string
	}{
		"the last part missing": {
			func(directory string) {
				if removeError := os.Remove(filepath.Join(directory, "cases_3.json")); removeError != nil {
					test.Fatal(removeError)
				}
			},
			"cases_1.json says it is part 1 of 3, and it is part 1 of the 2 on disk",
		},
		"a part disagreeing on the count": {
			func(directory string) { writePart(test, directory, "cases_2.json", 2, 4) },
			"cases_2.json says it is part 2 of 4, and it is part 2 of the 3 on disk",
		},
		"a part numbered wrong": {
			func(directory string) { writePart(test, directory, "cases_2.json", 3, 3) },
			"cases_2.json says it is part 3 of 3, and it is part 2 of the 3 on disk",
		},
		"a part saying nothing": {
			func(directory string) {
				path := filepath.Join(directory, "cases_3.json")
				if writeError := os.WriteFile(path, []byte(`{"inputs": []}`), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			},
			"cases_3.json says it is part 0 of 0",
		},
		"a compressed part": {
			func(directory string) {
				path := filepath.Join(directory, "cases_4.json.gz")
				if writeError := os.WriteFile(path, gzipped(test, "{}"), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			},
			"cases_4.json.gz is compressed",
		},
	} {
		directory := test.TempDir()
		writeParts(test, directory, "cases_", 3)
		// The control: the set as written reads, so what fails below is the plant.
		if _, partsError := Parts(directory, "cases_"); partsError != nil {
			test.Fatalf("%s: the set before the plant was refused: %v", name, partsError)
		}
		testCase.plant(directory)
		if _, partsError := Parts(directory, "cases_"); partsError == nil ||
			!strings.Contains(partsError.Error(), testCase.want) {
			test.Errorf("%s: want an error naming %q, got %v", name, testCase.want, partsError)
		}
	}
}

func TestFingerprintIsADigestPrefixAndALength(test *testing.T) {
	test.Parallel()
	// Each SHA-256's first 16 hex digits as `printf '<text>' | shasum -a 256` prints them, and the text's length
	// in bytes, three for the one character 中.
	for text, want := range map[string]string{
		"":       "e3b0c44298fc1c14:0",
		"中":      "a567bdaa11367f26:3",
		"a: b\n": "52c9693cf3f219d5:5",
	} {
		if fingerprint := Fingerprint(text); fingerprint != want {
			test.Errorf("%q: got %s, want %s", text, fingerprint, want)
		}
	}
}

// CanonicalJson writes what an untyped decode and encode would: keys sorted, every number as the float64 it
// parses to, and encoding/json's escapes, so a fingerprint taken of it equals one taken by the recorders.
func TestCanonicalJsonWritesWhatAnUntypedRoundTripWrites(test *testing.T) {
	test.Parallel()
	for input, want := range map[string]string{
		`{"b": 1.0, "a": [1e2, "<&>", null, true]}`: `{"a":[100,"\u003c\u0026\u003e",null,true],"b":1}`,
		`[0.1, -0, 12345678901234567890]`:           `[0.1,-0,12345678901234567000]`,
		`"text"`:                                    `"text"`,
	} {
		canonical, canonicalError := CanonicalJson(json.RawMessage(input))
		if canonicalError != nil || canonical != want {
			test.Errorf("CanonicalJson(%s) = %s, %v; want %s", input, canonical, canonicalError, want)
		}
	}
}
