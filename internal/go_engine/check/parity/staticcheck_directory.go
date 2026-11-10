package parity

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// staticcheckDirectory is the go command's answer, asked once per test binary, since every package's tests read the
// same module.
var staticcheckDirectory = sync.OnceValues(func() (string, error) {
	command := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "honnef.co/go/tools")
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return "", fmt.Errorf("asking the go command where honnef.co/go/tools is: %w: %s", runError,
			strings.TrimSpace(standardError.String()))
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" {
		return "", fmt.Errorf("the go command names no directory for honnef.co/go/tools, so the module is not in " +
			"the module cache: `go mod download honnef.co/go/tools` fetches it")
	}
	return directory, nil
})

// StaticcheckDirectory is the directory of honnef.co/go/tools, staticcheck's module, as this module resolves it: the
// module cache's copy of the release go.mod requires, where a port's test reads staticcheck's own fixtures. The module
// cache is read-only, so a test that writes beside a fixture copies it into a directory of its own first. It asks the
// go command, run in the calling test's package directory, which lies in this module, and fails the test with the
// command's own error when it cannot answer, or with why when it answers with no directory.
func StaticcheckDirectory(test *testing.T) string {
	test.Helper()
	directory, listError := staticcheckDirectory()
	if listError != nil {
		test.Fatal(listError)
	}
	return directory
}
