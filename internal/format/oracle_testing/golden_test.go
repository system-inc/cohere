package oracle_testing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGolden writes contents as plain JSON to path, making its directory, and returns path.
func writeGolden[Contents any](test *testing.T, path string, contents Contents) string {
	test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	encoded, marshalError := json.Marshal(contents)
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	if writeError := os.WriteFile(path, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// writeParts writes a golden split into one part per answer under directory/golden, each stamped with digest
// and numbered as a split golden's parts are, and returns the golden's base path.
func writeParts(test *testing.T, directory string, digest string, keys ...string) string {
	test.Helper()
	base := filepath.Join(directory, "golden")
	for index, key := range keys {
		writeGolden(test, filepath.Join(base, fmt.Sprintf("part_%d.json", index+1)), record{
			Digest:  digest,
			Part:    index + 1,
			Parts:   len(keys),
			Answers: map[string]Answer{key: {Output: key}},
		})
	}
	return base
}

// TestAGoldenFromOtherBundlesIsRefused is the property the golden exists to keep: answers recorded from
// bundles other than the ones named are never compared against.
func TestAGoldenFromOtherBundlesIsRefused(test *testing.T) {
	test.Parallel()
	base := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, base+".json", record{Digest: "recorded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, _, readError := read(
		base,
		"embedded",
	); readError == nil || !strings.Contains(readError.Error(), "recorded from Prettier bundles recorded") {
		test.Fatalf("a golden from other bundles was read: %v", readError)
	}
	// The control: the same file, with the digest it was recorded under, reads.
	answers, _, readError := read(base, "recorded")
	if readError != nil || answers["k"].Output != "x" {
		test.Fatalf("a golden from these bundles was refused: %v, %v", answers, readError)
	}
}

// TestAMissingGoldenIsRefused: no golden is a failure, never an empty pass.
func TestAMissingGoldenIsRefused(test *testing.T) {
	test.Parallel()
	if _, _, readError := read(filepath.Join(test.TempDir(), "absent"), "embedded"); readError == nil {
		test.Fatal("a missing golden read as empty")
	}
}

// TestAGoldenNamedInAnotherCaseIsRefused: a golden whose stored name differs from its test's only in case
// fails on every disk, not only on a case-sensitive one where the open itself would miss it, whole or split.
func TestAGoldenNamedInAnotherCaseIsRefused(test *testing.T) {
	test.Parallel()
	whole := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, whole+".json", record{Digest: "embedded", Answers: map[string]Answer{"k": {Output: "k"}}})
	split := writeParts(test, test.TempDir(), "embedded", "k")
	for _, base := range []string{whole, split} {
		if _, _, readError := read(
			filepath.Join(filepath.Dir(base), "Golden"),
			"embedded",
		); readError == nil || !strings.Contains(readError.Error(), "rename it to Golden") {
			test.Errorf("a golden stored as %s was read as Golden: %v", base, readError)
		}
		// The control: the name as stored reads.
		if answers, _, readError := read(base, "embedded"); readError != nil || answers["k"].Output != "k" {
			test.Errorf("a golden read by its stored name, %s, was refused: %v, %v", base, answers, readError)
		}
	}
}

// TestACompressedGoldenIsRefused: a gzipped golden, whole or a part, is refused before anything reads it, since
// compressed fixtures never land on a public repository.
func TestACompressedGoldenIsRefused(test *testing.T) {
	test.Parallel()
	whole := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, whole+".json.gz", record{Digest: "embedded", Answers: map[string]Answer{}})
	split := writeParts(test, test.TempDir(), "embedded", "a")
	writeGolden(test, filepath.Join(split, "part_2.json.gz"), record{Digest: "embedded", Part: 2, Parts: 2})
	for _, base := range []string{whole, split} {
		if _, _, readError := read(base, "embedded"); readError == nil ||
			!strings.Contains(readError.Error(), "never land on a public repository") {
			test.Errorf("a compressed golden at %s was read: %v", base, readError)
		}
	}
}

// TestASplitGoldenReadsEveryPart: a golden split into parts reads as one, every part's answers together.
func TestASplitGoldenReadsEveryPart(test *testing.T) {
	test.Parallel()
	base := writeParts(test, test.TempDir(), "embedded", "a", "b", "c")
	answers, path, readError := read(base, "embedded")
	if readError != nil || len(answers) != 3 || answers["c"].Output != "c" || path != base {
		test.Fatalf("a split golden read as %v from %s, %v", answers, path, readError)
	}
}

// TestASplitGoldenThatDoesNotAddUpIsRefused: each part vouches for the rest, so a part missing, a count that
// disagrees, a part numbered wrong, or anything beside the parts fails rather than reading as fewer answers.
func TestASplitGoldenThatDoesNotAddUpIsRefused(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		plant func(base string)
		want  string
	}{
		"the last part missing": {
			func(base string) {
				if removeError := os.Remove(filepath.Join(base, "part_3.json")); removeError != nil {
					test.Fatal(removeError)
				}
			},
			"part_1.json says it is part 1 of 3, and it is part 1 of the 2 on disk",
		},
		"a middle part missing": {
			func(base string) {
				if removeError := os.Remove(filepath.Join(base, "part_2.json")); removeError != nil {
					test.Fatal(removeError)
				}
			},
			"part_3.json among them, so a part before it is missing",
		},
		"the parts disagreeing on their count": {
			func(base string) {
				writeGolden(test, filepath.Join(base, "part_2.json"), record{Digest: "embedded", Part: 2, Parts: 4})
			},
			"part_2.json says it is part 2 of 4, and it is part 2 of the 3 on disk",
		},
		"a part numbered wrong": {
			func(base string) {
				writeGolden(test, filepath.Join(base, "part_2.json"), record{Digest: "embedded", Part: 3, Parts: 3})
			},
			"part_2.json says it is part 3 of 3",
		},
		"a part from other bundles": {
			func(base string) {
				writeGolden(test, filepath.Join(base, "part_3.json"), record{Digest: "other", Part: 3, Parts: 3})
			},
			"part_3.json was recorded from Prettier bundles other",
		},
		"a key answered in two parts": {
			func(base string) {
				writeGolden(test, filepath.Join(base, "part_2.json"), record{
					Digest:  "embedded",
					Part:    2,
					Parts:   3,
					Answers: map[string]Answer{"a": {Output: "a"}},
				})
			},
			"part_2.json answers a, which an earlier part answers too",
		},
		"a stray file beside the parts": {
			func(base string) { writeGolden(test, filepath.Join(base, "notes.json"), record{}) },
			"holds notes.json, and a split golden holds only part_<n>.json",
		},
		"a whole golden beside the parts": {
			func(base string) { writeGolden(test, base+".json", record{Digest: "embedded"}) },
			"are both on disk",
		},
		"a misspelled field": {
			func(base string) {
				writeGolden(test, filepath.Join(base, "part_3.json"), struct {
					Digest    string `json:"digest"`
					Part      int    `json:"part"`
					PartCount int    `json:"part_count"`
				}{"embedded", 3, 3})
			},
			`unknown field "part_count"`,
		},
	} {
		base := writeParts(test, test.TempDir(), "embedded", "a", "b", "c")
		testCase.plant(base)
		if _, _, readError := read(base, "embedded"); readError == nil ||
			!strings.Contains(readError.Error(), testCase.want) {
			test.Errorf("%s: want an error naming %q, got %v", name, testCase.want, readError)
		}
	}
}

// TestAWholeGoldenThatSaysItIsAPartIsRefused: a whole golden numbered as a part is a part out of place.
func TestAWholeGoldenThatSaysItIsAPartIsRefused(test *testing.T) {
	test.Parallel()
	base := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, base+".json", record{Digest: "embedded", Part: 1, Parts: 2})
	if _, _, readError := read(base, "embedded"); readError == nil ||
		!strings.Contains(readError.Error(), "says it is part 1 of 2") {
		test.Fatalf("a whole golden numbered as a part was read: %v", readError)
	}
}

// TestKeysDoNotRunTogether: inputs that concatenate to the same text are different keys.
func TestKeysDoNotRunTogether(test *testing.T) {
	test.Parallel()
	if Key("ab", "c") == Key("a", "bc") {
		test.Fatal("two different inputs share a key")
	}
	// Two calls, so the comparison is of two keys computed apart rather than one expression with itself.
	first := Key("a", "b")
	second := Key("a", "b")
	if first != second {
		test.Fatal("one input has two keys")
	}
}

// recordingTest is a testing.TB whose errors are kept rather than failing the test that holds it, so a
// golden's own complaint can be asserted.
type recordingTest struct {
	testing.TB
	errors []string
}

func (recording *recordingTest) Errorf(format string, arguments ...any) {
	recording.errors = append(recording.errors, fmt.Sprintf(format, arguments...))
}

// TestAnAnswerIsWhatWasRecorded: a recorded output comes back as the output and a recorded refusal as the
// error, while an answer never recorded fails the test and is ErrNotRecorded, never an empty pass.
func TestAnAnswerIsWhatWasRecorded(test *testing.T) {
	test.Parallel()
	recording := &recordingTest{TB: test}
	golden := &Golden{
		test:    recording,
		path:    "golden.json",
		answers: map[string]Answer{Key("printed"): {Output: "x"}, Key("refused"): {Error: "SyntaxError"}},
		asked:   map[string]bool{},
	}
	if output, answerError := golden.Answer(Key("printed")); answerError != nil || output != "x" {
		test.Errorf("a recorded output answered %q, %v", output, answerError)
	}
	if _, answerError := golden.Answer(Key("refused")); answerError == nil || answerError.Error() != "SyntaxError" {
		test.Errorf("a recorded refusal answered %v", answerError)
	}
	if len(recording.errors) != 0 {
		test.Fatalf("recorded answers failed the test: %v", recording.errors)
	}
	if _, answerError := golden.Answer(Key("never")); !errors.Is(answerError, ErrNotRecorded) ||
		len(recording.errors) != 1 {
		test.Errorf("an answer never recorded gave %v and failed the test %d times", answerError, len(recording.errors))
	}
}

// TestAnAnswerNothingAskedForFailsTheTest: once a test opening a golden ends clean, each answer it never asked
// for fails it, naming the golden and the answer, as package recorded fails an answer no input has.
func TestAnAnswerNothingAskedForFailsTheTest(test *testing.T) {
	test.Parallel()
	if filteredToSubtests() {
		test.Skip("this run is filtered to subtests, and a filtered run asks no golden for every answer")
	}
	base := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, base+".json", record{
		Digest:  pinnedBundlesDigest,
		Answers: map[string]Answer{Key("asked"): {Output: "x"}, Key("unasked"): {Output: "y"}},
	})
	for _, testCase := range []struct {
		name string
		ask  []string
		want int
	}{
		{"one of two asked", []string{"asked"}, 1},
		{"both asked", []string{"asked", "unasked"}, 0},
	} {
		recording := &recordingTest{}
		// Not parallel: the subtest must have ended, its cleanup and all, when Run returns, so the errors its
		// golden reported can be read here.
		test.Run(testCase.name, func(subtest *testing.T) {
			recording.TB = subtest
			golden := open(recording, base)
			for _, ask := range testCase.ask {
				if _, answerError := golden.Answer(Key(ask)); answerError != nil {
					subtest.Fatal(answerError)
				}
			}
		})
		if len(recording.errors) != testCase.want {
			test.Errorf("%s: the golden failed the test %d times: %v", testCase.name, len(recording.errors),
				recording.errors)
		}
		if testCase.want == 1 && (!strings.Contains(recording.errors[0], Key("unasked")) ||
			!strings.Contains(recording.errors[0], base+".json")) {
			test.Errorf(
				"%s: the failure names neither the answer nor the golden: %s",
				testCase.name,
				recording.errors[0],
			)
		}
	}
}

// TestAGoldenStampedOtherwiseIsRefused: Open holds every golden to the pinned digest, so one stamped with any
// other is refused, not compared.
func TestAGoldenStampedOtherwiseIsRefused(test *testing.T) {
	test.Parallel()
	base := filepath.Join(test.TempDir(), "golden")
	writeGolden(test, base+".json", record{Digest: "recorded", Answers: map[string]Answer{"k": {Output: "x"}}})
	if _, openError := openAt(test, base); openError == nil ||
		!strings.Contains(openError.Error(), "pinned to "+pinnedBundlesDigest) {
		test.Fatalf("a golden stamped with another digest was opened: %v", openError)
	}
	// The control: the same answers stamped with the pinned digest open.
	pinned := filepath.Join(test.TempDir(), "golden")
	writeGolden(
		test,
		pinned+".json",
		record{Digest: pinnedBundlesDigest, Answers: map[string]Answer{"k": {Output: "x"}}},
	)
	golden, openError := openAt(test, pinned)
	if openError != nil || golden.answers["k"].Output != "x" {
		test.Fatalf("a golden stamped with the pinned digest was refused: %v", openError)
	}
}
