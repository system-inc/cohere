package prettier

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/needs"
)

// TestCost measures formatting throughput per byte rather than per file.
//
// Per-file cost is not a property of the formatter, it is a property of the sample: a corpus of
// 9.5 KB files will report roughly twice the per-file cost of a corpus of 4.3 KB files while the
// engine does exactly the same work per byte. Two honest measurements of the same engine can
// therefore disagree by 2x with neither being wrong, which is what happened here.
//
//	COHERE_COST_LIST=/path/to/list go test ./internal/prettier/ -run TestCost -v
func TestCost(test *testing.T) {
	test.Parallel()

	listPath := needs.OptIn(test, "COHERE_COST_LIST", "it times the engine over a list of files")
	listBytes, readError := os.ReadFile(listPath)
	if readError != nil {
		test.Fatalf("reading the list: %v", readError)
	}

	constructionStart := time.Now()
	engine := newTestEngine(test)
	construction := time.Since(constructionStart)

	var totalBytes int
	var formatted int
	var formatting time.Duration
	for _, path := range strings.Split(strings.TrimSpace(string(listBytes)), "\n") {
		path = strings.TrimSpace(path)
		if path == "" || !engine.Handles(path) {
			continue
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		start := time.Now()
		if _, formatError := engine.Format(path, string(source)); formatError != nil {
			test.Errorf("%s: %v", path, formatError)
			continue
		}
		formatting += time.Since(start)
		totalBytes += len(source)
		formatted++
	}

	if formatted == 0 {
		test.Fatal("measured no files, so this run says nothing")
	}
	test.Logf("construction %v (once)", construction)
	test.Logf("files %d, bytes %d, mean file %d bytes", formatted, totalBytes, totalBytes/formatted)
	test.Logf("formatting %v total, %v per file, %.1f KB/s",
		formatting, formatting/time.Duration(formatted),
		float64(totalBytes)/1024/formatting.Seconds())
}
