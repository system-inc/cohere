// Package oracle_testing keeps what the Prettier fork answered for a test's fixtures in a checked-in golden
// file, so the test compares bytes and runs no JavaScript (#nxgt2ca).
//
// The live oracle formatted every fixture under every option set on every run, and yaml, css and markdown
// spent minutes of the module's test time in it. Its answers were recorded once, through the fork's bundles
// run in-process, keyed by everything an answer depends on, and stamped with the bundles' digest. Those bundles
// have left cohere (#mjecaaw), so the goldens are read-only: they stand as the fork's word, and a golden
// stamped with any digest but pinnedBundlesDigest is refused rather than compared.
//
// A golden is plain JSON, testdata/oracle/<name>.json, or, past 50 KB, numbered parts under
// testdata/oracle/<name>/, each named part_<n>.json and saying which part it is of how many, so a part lost
// fails rather than reading as answers never recorded. Compressed goldens are refused: cohere is public, and
// its landing gate turns away compressed fixtures on a public repository, since what they hold cannot be read
// in review.
//
// The checks themselves are untouched: every fixture is still compared under every option set, against
// the same bytes the oracle gave. A golden also holds the check to every answer it recorded: one that nothing
// asked for fails the test, as package recorded fails an answer no input has, so a fixture dropped or edited
// leaves no answer behind that compares against nothing.
package oracle_testing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_options"
)

// pinnedBundlesDigest is the digest of the Prettier 3.9.6 fork bundles that recorded every golden here. The
// bundles are gone from cohere (#mjecaaw), so no golden can be recorded again; this digest is the stamp every
// golden must carry, and one stamped otherwise came from other bundles and is refused.
const pinnedBundlesDigest = "61573b94b6f4507420b448b348c232f988c86f60dc9b23d089ce3b179b2afc98"

// ErrNotRecorded is an answer asked for that the golden does not hold: the fixtures changed since it was
// recorded.
var ErrNotRecorded = errors.New("this answer was never recorded")

// partName is a part's file name under a split golden's directory, its number in the one group.
var partName = regexp.MustCompile(`^part_([1-9][0-9]*)\.json$`)

// Answer is one recorded answer: what the oracle printed, or the error it gave.
type Answer struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// record is a golden file's contents: a whole golden, or one part of a split one, which names its own number
// and the count of parts so that every part vouches for the others being there.
type record struct {
	Digest  string            `json:"digest"`
	Part    int               `json:"part,omitempty"`
	Parts   int               `json:"parts,omitempty"`
	Answers map[string]Answer `json:"answers"`
}

// Golden is one test's recorded answers. Its answers are only read, so tests running in parallel share it
// freely; what each asks for is kept under mutex, so the golden can say at the end what nothing asked for.
type Golden struct {
	test    testing.TB
	path    string
	answers map[string]Answer
	mutex   sync.Mutex
	asked   map[string]bool
}

// Open reads the golden named name under testdata/oracle, whole or in parts, which must be stamped with
// pinnedBundlesDigest. When the test ends clean, every answer the golden holds must have been asked for.
func Open(test testing.TB, name string) *Golden {
	test.Helper()
	return open(test, filepath.Join("testdata", "oracle", name))
}

// open is Open for the golden at base, its path without an extension.
func open(test testing.TB, base string) *Golden {
	test.Helper()
	golden, openError := openAt(test, base)
	if openError != nil {
		test.Fatal(openError)
	}
	test.Cleanup(golden.requireEveryAnswerAsked)
	return golden
}

// openAt is Open for the golden at base, its path without an extension.
func openAt(test testing.TB, base string) (*Golden, error) {
	answers, path, readError := read(base, pinnedBundlesDigest)
	if readError != nil {
		return nil, readError
	}
	return &Golden{test: test, path: path, answers: answers, asked: map[string]bool{}}, nil
}

// read is the answers of the golden at base, its path without an extension, and the path they were read from:
// base.json, or the directory base holding its parts. It is refused when compressed, when both or neither are
// on disk, when any part is missing, and when any file is stamped with a digest but digest.
func read(base string, digest string) (map[string]Answer, string, error) {
	if _, statError := os.Stat(base + ".json.gz"); statError == nil {
		return nil, "", compressedError(base + ".json.gz")
	}
	for _, path := range []string{base + ".json", base} {
		if caseError := storedExactly(path); caseError != nil {
			return nil, "", caseError
		}
	}
	_, wholeError := os.Stat(base + ".json")
	directory, directoryError := os.Stat(base)
	splitExists := directoryError == nil && directory.IsDir()
	switch {
	case wholeError == nil && splitExists:
		return nil, "", fmt.Errorf("%s.json and %s/ are both on disk, and a golden is one or the other", base, base)
	case splitExists:
		answers, readError := readParts(base, digest)
		return answers, base, readError
	case wholeError != nil:
		return nil, "", fmt.Errorf("%s.json holds no recorded oracle answers (%w)", base, wholeError)
	}
	contents, decodeError := decode(base+".json", digest)
	if decodeError != nil {
		return nil, "", decodeError
	}
	if contents.Part != 0 || contents.Parts != 0 {
		return nil, "", fmt.Errorf(
			"%s.json says it is part %d of %d, and a split golden's parts live under %s/",
			base,
			contents.Part,
			contents.Parts,
			base,
		)
	}
	return contents.Answers, base + ".json", nil
}

// storedExactly refuses a path whose stored name differs from it only in case. A golden is named for its test,
// byte for byte. A case-insensitive disk (macOS's) opens TestFormatSCSSMatchesTheFork.json for
// TestFormatScssMatchesTheFork and a case-sensitive one (a Linux test box, a Loom unit) does not, so a name
// that differs only in case passes here and fails there. The directory's own listing holds the name as stored,
// so the check reads it rather than trusting the open.
func storedExactly(path string) error {
	entries, readDirectoryError := os.ReadDir(filepath.Dir(path))
	if readDirectoryError != nil {
		// Ignored: a directory that does not list holds no golden, and the open that follows says so.
		return nil
	}
	exact, storedName := false, ""
	for _, entry := range entries {
		exact = exact || entry.Name() == filepath.Base(path)
		if strings.EqualFold(entry.Name(), filepath.Base(path)) {
			storedName = entry.Name()
		}
	}
	if !exact && storedName != "" {
		return fmt.Errorf(
			"%s is stored as %s, which only a case-insensitive disk opens: rename it to %s",
			path,
			storedName,
			filepath.Base(path),
		)
	}
	return nil
}

// compressedError is the refusal of a compressed golden at path.
func compressedError(path string) error {
	return fmt.Errorf(
		"%s is compressed, and compressed fixtures never land on a public repository: keep it as plain JSON, "+
			"split into part_<n>.json files under 50 KB",
		path,
	)
}

// readParts is the answers of the split golden under directory: part_1.json up to the count on disk, each
// stamped with digest and saying it is its own number of that count. Anything else there is refused, so a part
// deleted, a part added without renumbering the rest, or a stray file fails rather than reading as fewer answers.
func readParts(directory string, digest string) (map[string]Answer, error) {
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		return nil, readDirectoryError
	}
	// A file whose name starts with a dot is the disk's, never a part: macOS's Finder leaves a .DS_Store in any
	// directory it shows, and git ignores it, so it would fail the test on one machine and nowhere else.
	entries = slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return strings.HasPrefix(entry.Name(), ".") })
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s/ holds no parts", directory)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".gz") {
			return nil, compressedError(filepath.Join(directory, entry.Name()))
		}
		match := partName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("%s/ holds %s, and a split golden holds only part_<n>.json", directory, entry.Name())
		}
		// Ignored: the pattern admits only digits, so the number always parses.
		number, _ := strconv.Atoi(match[1])
		if number > len(entries) {
			return nil, fmt.Errorf(
				"%s/ holds %d files and %s among them, so a part before it is missing",
				directory,
				len(entries),
				entry.Name(),
			)
		}
	}
	answers := map[string]Answer{}
	for number := 1; number <= len(entries); number++ {
		path := filepath.Join(directory, fmt.Sprintf("part_%d.json", number))
		contents, decodeError := decode(path, digest)
		if decodeError != nil {
			return nil, decodeError
		}
		if contents.Part != number || contents.Parts != len(entries) {
			return nil, fmt.Errorf(
				"%s says it is part %d of %d, and it is part %d of the %d on disk",
				path,
				contents.Part,
				contents.Parts,
				number,
				len(entries),
			)
		}
		for key, answer := range contents.Answers {
			if _, repeated := answers[key]; repeated {
				return nil, fmt.Errorf("%s answers %s, which an earlier part answers too", path, key)
			}
			answers[key] = answer
		}
	}
	return answers, nil
}

// decode is the golden file at path, refused when it is stamped with any digest but digest. A field it does not
// declare is refused too, so a misspelled parts fails rather than reading as a whole golden.
func decode(path string, digest string) (record, error) {
	var contents record
	file, openError := os.Open(path)
	if openError != nil {
		return contents, fmt.Errorf("%s holds no recorded oracle answers (%w)", path, openError)
	}
	// Ignored: the golden is only read, so a failed close loses nothing already decoded.
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&contents); decodeError != nil {
		return contents, fmt.Errorf("%s is not a golden: %w", path, decodeError)
	}
	if contents.Digest != digest {
		return contents, fmt.Errorf(
			"%s was recorded from Prettier bundles %s, and every golden is pinned to %s",
			path,
			contents.Digest,
			digest,
		)
	}
	return contents, nil
}

// Key is an answer's key: every input it depends on, each prefixed with its length so no two inputs can
// run together into the same key.
func Key(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(hash, "%d\x00%s\x00", len(part), part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// Answer is the recorded answer for key, error and all. An answer the golden never recorded fails the test and
// returns ErrNotRecorded: the fixture asking for it is new or changed, and with the bundles gone its answer can
// only come from upstream some other way, such as a recording made outside cohere.
func (golden *Golden) Answer(key string) (string, error) {
	answer, recorded := golden.answers[key]
	if !recorded {
		golden.test.Errorf(
			"%s holds no answer for a fixture asked for here, so the fixture is new or changed since it was "+
				"recorded, and the bundles that recorded it are gone",
			golden.path,
		)
		return "", ErrNotRecorded
	}
	golden.mutex.Lock()
	golden.asked[key] = true
	golden.mutex.Unlock()
	if answer.Error != "" {
		return "", errors.New(answer.Error)
	}
	return answer.Output, nil
}

// requireEveryAnswerAsked fails the test for each answer the golden holds that nothing asked for: the fixture
// it was recorded for is gone or changed, so the answer compares against nothing. Only a whole clean run judges
// it. A test that failed or skipped may have stopped before asking, and a run filtered to some subtests (a
// pattern for test.run or test.skip naming one, with a slash) leaves the others' answers unasked.
func (golden *Golden) requireEveryAnswerAsked() {
	if golden.test.Failed() || golden.test.Skipped() || filteredToSubtests() {
		return
	}
	golden.mutex.Lock()
	defer golden.mutex.Unlock()
	var unasked []string
	for key := range golden.answers {
		if !golden.asked[key] {
			unasked = append(unasked, key)
		}
	}
	slices.Sort(unasked)
	for _, key := range unasked {
		golden.test.Errorf(
			"%s holds an answer for %s that nothing here asked for, so its fixture is gone or changed: "+
				"remove the answer",
			golden.path,
			key,
		)
	}
}

// filteredToSubtests says the run names subtests in test.run or test.skip, so a test runs only some of its
// subtests and the rest leave their answers unasked.
func filteredToSubtests() bool {
	for _, name := range []string{"test.run", "test.skip"} {
		if pattern := flag.Lookup(name); pattern != nil && strings.Contains(pattern.Value.String(), "/") {
			return true
		}
	}
	return false
}

// Formatter answers as the fork's formatter did under options, from the golden.
func (golden *Golden) Formatter(options format_options.Options) *Formatter {
	return &Formatter{golden: golden, options: options}
}
