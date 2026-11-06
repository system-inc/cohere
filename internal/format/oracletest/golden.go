// Package oracletest keeps what the embedded Prettier answered for a test's fixtures in a checked-in golden
// file, so the test compares bytes and runs no JavaScript (#nxgt2ca).
//
// The live oracle formatted every fixture under every option set on every run, and yaml, css and markdown
// spent minutes of the module's test time in it. Its answers change only when the bundles do, so they are
// recorded once, keyed by everything an answer depends on, and stamped with the bundles' digest. A golden
// recorded from other bundles fails the test loudly, naming the command that records it again, rather
// than comparing the native printers against an oracle nothing loads any more. Recording is its own run:
//
//	COHERE_UPDATE_ORACLE=1 go test ./internal/format/yaml
//
// The checks themselves are untouched: every fixture is still compared under every option set, against
// the same bytes the oracle gives.
package oracletest

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// UpdateVariable, set to anything, records the oracle's answers anew instead of reading them.
const UpdateVariable = "COHERE_UPDATE_ORACLE"

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
}

// Open reads the golden at testdata/oracle/<name>.json.gz for the bundles this build embeds. When recording,
// it starts empty and writes what the test asked for when the test ends.
func Open(test testing.TB, name string) *Golden {
	test.Helper()
	digest, bundlesDigestError := BundlesDigest()
	if bundlesDigestError != nil {
		test.Fatal(bundlesDigestError)
	}
	golden := &Golden{
		test:    test,
		path:    filepath.Join("testdata", "oracle", name+".json.gz"),
		digest:  digest,
		update:  os.Getenv(UpdateVariable) != "",
		answers: map[string]Answer{},
	}
	if golden.update {
		test.Cleanup(golden.write)
		return golden
	}
	answers, readError := read(golden.path, digest)
	if readError != nil {
		test.Fatalf(
			"%v: record them with %s=1 go test -run '^%s$' in this package",
			readError,
			UpdateVariable,
			test.Name(),
		)
	}
	golden.answers = answers
	return golden
}

// read is a golden's answers, refused when the bundles that recorded them are not these.
func read(path string, digest string) (map[string]Answer, error) {
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
			"%s was recorded from Prettier bundles %s, and this build embeds %s",
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
		output, computeError := compute()
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

// Engine answers as prettier.New(options) would, from the golden. The real engine is built only when
// recording, and only once.
func (golden *Golden) Engine(options format_options.Options) *Engine {
	return &Engine{golden: golden, options: options}
}

var bundlesDigest struct {
	once        sync.Once
	digest      string
	digestError error
}

// BundlesDigest is the digest of the Prettier bundles this build embeds, read once.
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
