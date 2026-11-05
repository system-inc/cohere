package edit

import (
	"fmt"
	"os"
)

// readFile reads a source file's text.
//
// Separated from its one caller so a test can state plainly that a missing file is an error rather
// than an empty string. An empty string parses fine, applies no fixes, and reports success — which
// is the exact shape of every silent-green failure in this project's history.
func readFile(fileName string) (string, error) {
	contents, readError := os.ReadFile(fileName)
	if readError != nil {
		return "", fmt.Errorf("reading %s: %w", fileName, readError)
	}
	return string(contents), nil
}
