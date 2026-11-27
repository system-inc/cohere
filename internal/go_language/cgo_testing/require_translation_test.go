package cgo_testing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
)

// fakeGo is a go that says CGO_ENABLED is FAKE_CGO and answers go list as FAKE_LIST says: "compiles", "refuses"
// as Loom's stand-in does, or "breaks" as a broken toolchain would. Its refusal is in our own words around the two
// markers translationUnavailable matches, `loom-runner:` and `never builds`, which are all of the runner's message
// the probe relies on.
const fakeGo = `#!/bin/sh
case "$1" in
env) echo "$FAKE_CGO" ;;
list)
	case "$FAKE_LIST" in
	compiles) echo '{"ImportPath":"unsafe"}' ;;
	refuses)
		echo "loom-runner: go $*: refused, since this stand-in never builds" >&2
		exit 1 ;;
	*) echo "go: cannot find GOROOT directory" >&2; exit 2 ;;
	esac ;;
esac
`

// Not parallel: each case puts its own go first on PATH, which the whole process shares.
func TestTranslationUnavailableSkipsOnlyWhereGoCannotTranslate(test *testing.T) {
	directory := test.TempDir()
	if writeError := executable_testing.WriteFile(
		filepath.Join(directory, "go"),
		[]byte(fakeGo),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, expected := range []struct {
		name, cgo, list string
		reason          string
		fails           bool
	}{
		{name: "a go that compiles", cgo: "1", list: "compiles"},
		{name: "no cgo", cgo: "0", list: "compiles", reason: `go env CGO_ENABLED is "0"`},
		{name: "a runner's stand-in", cgo: "1", list: "refuses", reason: "refuses to compile"},
		{name: "a broken toolchain", cgo: "1", list: "breaks", fails: true},
	} {
		test.Setenv("FAKE_CGO", expected.cgo)
		test.Setenv("FAKE_LIST", expected.list)
		reason, failure := translationUnavailable()
		if (failure != nil) != expected.fails {
			test.Errorf("%s: failure %v, want one %t", expected.name, failure, expected.fails)
		}
		if expected.reason == "" && reason != "" || !strings.Contains(reason, expected.reason) {
			test.Errorf("%s: reason %q, want one with %q", expected.name, reason, expected.reason)
		}
	}
}
