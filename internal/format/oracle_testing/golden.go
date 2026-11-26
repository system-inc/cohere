// Package oracle_testing keeps what the embedded Prettier answered for a test's fixtures in a checked-in golden
// file, so the test compares bytes and runs no JavaScript (#nxgt2ca).
//
// The live oracle formatted every fixture under every option set on every run, and yaml, css and markdown
// spent minutes of the module's test time in it. Its answers change only when the bundles do, so they are
// recorded once, keyed by everything an answer depends on, and stamped with the bundles' digest. Every golden
// is held to one pinned digest (see pinnedBundlesDigest), so a plain run reads no bundle, and a golden stamped
// with any other is refused rather than compared. Recording is its own run, refused unless the bundles it
// records from are the pinned ones:
//
//	COHERE_UPDATE_ORACLE=1 go test ./internal/format/yaml
//
// The checks themselves are untouched: every fixture is still compared under every option set, against
// the same bytes the oracle gives.
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
	"sync"
	"testing"

	"github.com/dop251/goja"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// UpdateVariable, set to anything, records the oracle's answers anew instead of reading them.
const UpdateVariable = "COHERE_UPDATE_ORACLE"

// pinnedBundlesDigest is the digest of the Prettier 3.9.6 fork bundles that recorded every golden
// here. It is pinned rather than computed because those bundles leave cohere (#mjecaaw): once they are gone no
// golden can be recorded again, and the goldens stand as the fork's word. A golden stamped with any other
// digest came from other bundles and is refused. Until the bundles are deleted a recording still runs, but
// only when the embedded bundles' live digest is this one, so nothing is recorded against changed bundles.
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

// Golden is one test's recorded answers.
type Golden struct {
	test    testing.TB
	path    string
	digest  string
	update  bool
	mutex   sync.Mutex
	answers map[string]Answer
	// unfinished is set when a recording's compute never returned: it panicked or called runtime.Goexit. A
	// panic reaches Cleanup before the test is marked failed, so without this write would save a golden
	// missing every answer after the panic, and the next plain run would read it as the fork's word.
	unfinished bool
	// The runtime Evaluate records through, built by loadRuntime on the first recording Evaluate and never on a
	// plain run. loadRuntime is a field so a test can hold that it is never called.
	loadRuntime  func() (*goja.Runtime, error)
	runtimeOnce  sync.Once
	runtimeMutex sync.Mutex
	runtime      *goja.Runtime
	runtimeError error
}

// Open reads the golden at testdata/oracle/<name>.json.gz, which must be stamped with pinnedBundlesDigest. A
// plain run reads no bundle. When recording, it starts empty and writes what the test asked for when the test
// ends, refusing first unless the embedded bundles are the pinned ones.
func Open(test testing.TB, name string) *Golden {
	test.Helper()
	golden, openError := openAt(
		test,
		filepath.Join("testdata", "oracle", name+".json.gz"),
		os.Getenv(UpdateVariable) != "",
		BundlesDigest,
	)
	if openError != nil {
		test.Fatal(openError)
	}
	return golden
}

// openAt is Open for the golden at path. bundlesDigest is asked for the embedded bundles' live digest only when
// recording; it is an argument so a test can hold that a plain run never asks it.
func openAt(
	test testing.TB,
	path string,
	update bool,
	bundlesDigest func() (string, error),
) (*Golden, error) {
	golden := &Golden{
		test:        test,
		path:        path,
		digest:      pinnedBundlesDigest,
		update:      update,
		answers:     map[string]Answer{},
		loadRuntime: loadBundleRuntime,
	}
	if golden.update {
		liveDigest, bundlesDigestError := bundlesDigest()
		if bundlesDigestError != nil {
			return nil, fmt.Errorf("not recording %s: %w", path, bundlesDigestError)
		}
		if liveDigest != golden.digest {
			return nil, fmt.Errorf(
				"not recording %s: the embedded Prettier bundles are %s, and every golden is pinned to %s",
				path,
				liveDigest,
				golden.digest,
			)
		}
		test.Cleanup(golden.write)
		return golden, nil
	}
	answers, readError := read(golden.path, golden.digest)
	if readError != nil {
		return nil, fmt.Errorf(
			"%w: record them with %s=1 go test -run '^%s$' in this package",
			readError,
			UpdateVariable,
			test.Name(),
		)
	}
	golden.answers = answers
	return golden, nil
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

// write saves what this run recorded. Keys are sorted by the encoder, so recording twice from the same
// bundles writes the same bytes.
func (golden *Golden) write() {
	if golden.test.Failed() {
		golden.test.Logf("not recording %s, since the test failed", golden.path)
		return
	}
	golden.mutex.Lock()
	unfinished := golden.unfinished
	golden.mutex.Unlock()
	if unfinished {
		golden.test.Errorf("not recording %s, since an oracle answer's compute never returned", golden.path)
		return
	}
	if createDirectoryError := os.MkdirAll(filepath.Dir(golden.path), 0o755); createDirectoryError != nil {
		golden.test.Errorf("recording %s: %v", golden.path, createDirectoryError)
		return
	}
	file, createError := os.Create(golden.path)
	if createError != nil {
		golden.test.Errorf("recording %s: %v", golden.path, createError)
		return
	}
	// The close is checked, since a golden whose last bytes never landed would read as truncated next run.
	defer func() {
		if fileCloseError := file.Close(); fileCloseError != nil {
			golden.test.Errorf("recording %s: %v", golden.path, fileCloseError)
		}
	}()
	zipped, newWriterError := gzip.NewWriterLevel(file, gzip.BestCompression)
	if newWriterError != nil {
		golden.test.Errorf("recording %s: %v", golden.path, newWriterError)
		return
	}
	encoder := json.NewEncoder(zipped)
	encoder.SetIndent("", " ")
	if encodeError := encoder.Encode(record{Digest: golden.digest, Answers: golden.answers}); encodeError != nil {
		golden.test.Errorf("recording %s: %v", golden.path, encodeError)
		return
	}
	if closeError := zipped.Close(); closeError != nil {
		golden.test.Errorf("recording %s: %v", golden.path, closeError)
		return
	}
	golden.test.Logf("recorded %d oracle answers in %s", len(golden.answers), golden.path)
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

// Answer is the recorded answer for key. When recording, compute is asked, and what it says is recorded,
// error and all. An answer the golden never recorded fails the test and returns ErrNotRecorded.
func (golden *Golden) Answer(key string, compute func() (string, error)) (string, error) {
	if golden.update {
		// Set only once compute returns, so a panic or a Goexit inside it leaves the golden unwritten.
		finished := false
		defer func() {
			if !finished {
				golden.mutex.Lock()
				golden.unfinished = true
				golden.mutex.Unlock()
			}
		}()
		output, computeError := compute()
		finished = true
		answer := Answer{Output: output}
		if computeError != nil {
			answer = Answer{Error: computeError.Error()}
		}
		golden.mutex.Lock()
		golden.answers[key] = answer
		golden.mutex.Unlock()
		return output, computeError
	}
	golden.mutex.Lock()
	answer, recorded := golden.answers[key]
	golden.mutex.Unlock()
	if !recorded {
		golden.test.Errorf(
			"an oracle answer %s never recorded was asked for, so the fixtures changed: record them again with %s=1",
			golden.path,
			UpdateVariable,
		)
		return "", ErrNotRecorded
	}
	if answer.Error != "" {
		return "", errors.New(answer.Error)
	}
	return answer.Output, nil
}

// Formatter answers as prettier.New(options) would, from the golden. The real formatter is built only when
// recording, and only once.
func (golden *Golden) Formatter(options format_options.Options) *Formatter {
	return &Formatter{golden: golden, options: options}
}

var bundlesDigest struct {
	once        sync.Once
	digest      string
	digestError error
}

// BundlesDigest is the digest of the Prettier bundles this build embeds, read once, and only by a recording.
func BundlesDigest() (string, error) {
	bundlesDigest.once.Do(func() {
		bundles, bundlesError := prettier.Bundles()
		if bundlesError != nil {
			bundlesDigest.digestError = bundlesError
			return
		}
		bundlesDigest.digest, bundlesDigest.digestError = prettier.DigestBundles(bundles.Files)
	})
	return bundlesDigest.digest, bundlesDigest.digestError
}
