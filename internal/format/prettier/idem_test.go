package prettier

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
)

// TestFormatIsIdempotent checks that formatting formatted output changes nothing.
//
// A formatter whose second pass differs from its first would report writes forever, and the count
// would be honest while the tree never converged.
func TestFormatIsIdempotent(test *testing.T) {
	test.Parallel()

	listPath := needs.OptIn(test, "COHERE_IDEM_LIST", "it formats a list of files twice and compares them")
	engine := newTestEngine(test)
	listBytes, listReadError := os.ReadFile(listPath)
	if listReadError != nil {
		test.Fatalf("reading the list: %v", listReadError)
	}
	var checked int
	var unstable int
	var changedFromSource int
	for _, path := range strings.Split(strings.TrimSpace(string(listBytes)), "\n") {
		path = strings.TrimSpace(path)
		if path == "" || !engine.Handles(path) {
			continue
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		once, formatError := engine.Format(path, string(source))
		if formatError != nil {
			continue
		}
		twice, formatError := engine.Format(path, once)
		if formatError != nil {
			continue
		}
		checked++
		if once != string(source) {
			changedFromSource++
		}
		if once != twice {
			unstable++
			test.Errorf("%s: second pass differs from first", path)
		}
	}
	test.Logf("checked=%d changedFromSource=%d unstable=%d", checked, changedFromSource, unstable)
}
