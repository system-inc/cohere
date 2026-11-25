// Package flagged holds each form of another tool's suppression, and each is reported at the directive's first word.
package flagged

import "os"

//lint:ignore U1000 kept for a caller that may come // want "^`//lint:ignore` is staticcheck's suppression"
func kept() {}

//lint:file-ignore U1000 everything here is used // want "^`//lint:file-ignore` is staticcheck's suppression"

//lint:IGNORE U1000 a malformed command is still staticcheck's directive // want "^`//lint:IGNORE` is staticcheck's"

// unread swallows a read error.
//
//nolint:nilerr // want "^`//nolint:nilerr` is golangci-lint's suppression"
func unread(path string) error {
	if _, readError := os.ReadFile(path); readError != nil {
		return nil //nolint:nilerr // want "^`//nolint:nilerr` is golangci-lint's suppression"
	}
	return nil
}

//nolint:errcheck,gosec // want "^`//nolint:errcheck,gosec` is golangci-lint's suppression"
var several = 1

var trailingBare = 2 //nolint // want "^`//nolint` is golangci-lint's suppression"

// want +1 "^`//nolint` is golangci-lint's suppression, and cohere checks neither its reason nor whether"
//nolint
var bare = 3
