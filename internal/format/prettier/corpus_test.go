package prettier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
)

// TestCorpus formats a real file list with the shipped engine and writes each result, so the output
// can be compared byte for byte against the Node CLI outside the test.
//
// Off by default and driven by environment variables, because it needs a checkout of the tree it
// measures. A measuring run is not a unit test: it proves this package agrees with Node on real
// files, which is the acceptance claim, and it must never be confused with the fast controls above.
//
// The reason it exists at all is that the numbers gathered before this package existed were gathered
// by a separate probe. Same bundles and same settings is an assumption, not a measurement, and this
// package's claim has to be measured through this package's code.
//
//	COHERE_CORPUS_LIST=/path/to/list COHERE_CORPUS_OUT=/path/to/dir go test ./internal/prettier/ -run TestCorpus -v
func TestCorpus(test *testing.T) {
	test.Parallel()

	listPath := needs.OptIn(test, "COHERE_CORPUS_LIST", "it measures the engine over a list of files")
	outDirectory := needs.OptIn(test, "COHERE_CORPUS_OUT", "it writes the measurement's outputs there")

	engine := newTestEngine(test)
	listBytes, readError := os.ReadFile(listPath)
	if readError != nil {
		test.Fatalf("reading the list: %v", readError)
	}

	var formatted int
	var failed int
	var refused int
	for _, path := range strings.Split(strings.TrimSpace(string(listBytes)), "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !engine.Handles(path) {
			refused++
			continue
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		text, formatError := engine.Format(path, string(source))
		if formatError != nil {
			failed++
			test.Errorf("%s: %v", path, formatError)
			continue
		}
		flat := strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_")
		if writeError := os.WriteFile(filepath.Join(outDirectory, flat), []byte(text), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", flat, writeError)
		}
		formatted++
	}

	// A corpus run that formatted nothing is vacuous, and it reads exactly like a clean one.
	if formatted == 0 {
		test.Fatalf("the corpus formatted no files at all (refused %d, failed %d)", refused, failed)
	}
	test.Logf("formatted=%d failed=%d refused=%d", formatted, failed, refused)
}
