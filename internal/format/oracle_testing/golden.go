// Package oracle_testing keeps what the Prettier fork answered for a test's fixtures in a checked-in golden
// file, so the test compares bytes and runs no JavaScript (#nxgt2ca).
//
// The live oracle formatted every fixture under every option set on every run, and yaml, css and markdown
// spent minutes of the module's test time in it. Its answers were recorded once, through the fork's bundles
// run in-process, keyed by everything an answer depends on, and stamped with the bundles' digest. Those bundles
// have left cohere (#mjecaaw), so the goldens are read-only: they stand as the fork's word, and a golden
// stamped with any digest but pinnedBundlesDigest is refused rather than compared.
//
// The checks themselves are untouched: every fixture is still compared under every option set, against
// the same bytes the oracle gave.
package oracle_testing

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Answer is one recorded answer: what the oracle printed, or the error it gave.
type Answer struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// record is a golden file's contents.
type record struct {
	Digest  string            `json:"digest"`
	Answers map[string]Answer `json:"answers"`
}

// Golden is one test's recorded answers. It is only read, so tests running in parallel share it freely.
type Golden struct {
	test    testing.TB
	path    string
	answers map[string]Answer
}

// Open reads the golden at testdata/oracle/<name>.json.gz, which must be stamped with pinnedBundlesDigest.
func Open(test testing.TB, name string) *Golden {
	test.Helper()
	golden, openError := openAt(test, filepath.Join("testdata", "oracle", name+".json.gz"))
	if openError != nil {
		test.Fatal(openError)
	}
	return golden
}

// openAt is Open for the golden at path.
func openAt(test testing.TB, path string) (*Golden, error) {
	answers, readError := read(path, pinnedBundlesDigest)
	if readError != nil {
		return nil, readError
	}
	return &Golden{test: test, path: path, answers: answers}, nil
}

// read is a golden's answers, refused when it is stamped with any digest but digest.
func read(path string, digest string) (map[string]Answer, error) {
	// A golden is named for its test, byte for byte. A case-insensitive disk (macOS's) opens
	// TestFormatSCSSMatchesTheFork.json.gz for TestFormatScssMatchesTheFork and a case-sensitive one (a Linux
	// test box, a Loom unit) does not, so a name that differs only in case passes here and fails there. The
	// directory's own listing holds the name as stored, so the check reads it rather than trusting the open.
	if entries, readDirectoryError := os.ReadDir(filepath.Dir(path)); readDirectoryError == nil {
		storedExactly, storedName := false, ""
		for _, entry := range entries {
			storedExactly = storedExactly || entry.Name() == filepath.Base(path)
			if strings.EqualFold(entry.Name(), filepath.Base(path)) {
				storedName = entry.Name()
			}
		}
		if !storedExactly && storedName != "" {
			return nil, fmt.Errorf(
				"%s is stored as %s, which only a case-insensitive disk opens: rename it to %s",
				path,
				storedName,
				filepath.Base(path),
			)
		}
	}
	file, openError := os.Open(path)
	if openError != nil {
		return nil, fmt.Errorf("%s holds no recorded oracle answers (%w)", path, openError)
	}
	// Ignored: the golden is only read, so a failed close loses nothing already decoded.
	defer file.Close()
	unzipped, gzipError := gzip.NewReader(file)
	if gzipError != nil {
		return nil, fmt.Errorf("%s is not a golden: %w", path, gzipError)
	}
	var contents record
	if decodeError := json.NewDecoder(unzipped).Decode(&contents); decodeError != nil {
		return nil, fmt.Errorf("%s is not a golden: %w", path, decodeError)
	}
	if contents.Digest != digest {
		return nil, fmt.Errorf(
			"%s was recorded from Prettier bundles %s, and every golden is pinned to %s",
			path,
			contents.Digest,
			digest,
		)
	}
	return contents.Answers, nil
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
	if answer.Error != "" {
		return "", errors.New(answer.Error)
	}
	return answer.Output, nil
}

// Formatter answers as the fork's formatter did under options, from the golden.
func (golden *Golden) Formatter(options format_options.Options) *Formatter {
	return &Formatter{golden: golden, options: options}
}
