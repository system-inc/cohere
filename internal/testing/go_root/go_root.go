// Package go_root finds the Go toolchain's root for a test, in this process, so a test that needs `GOROOT` starts no
// `go env` to read one value.
//
// go/build's own root is empty in a test binary built with `-trimpath`, as cohere-dev builds every one, unless the
// `GOROOT` variable is set, and go test sets none. What go test does set is PATH, with the toolchain's bin first
// (cmd/go's base.AppendPATH), so the go found there sits in the root's bin. A root found that way is checked before
// it is used: one that holds no go in its bin or no runtime source is refused, never guessed past.
//
// It reads the toolchain's cgo setting and C compiler in process too (CgoEnabled, CCompiler), by cmd/go's own rules.
package go_root

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Find returns the toolchain's root: go/build's when it has one, and otherwise the parent of the bin holding the go
// on the path, its symbolic links resolved. Either is checked to be a root (checkRoot). It is for TestMain, which has
// no test to fail; a test calls GoRoot.
func Find() (string, error) {
	if build.Default.GOROOT != "" {
		return build.Default.GOROOT, checkRoot(build.Default.GOROOT, "go/build's GOROOT")
	}
	goExecutable, lookPathError := exec.LookPath("go")
	if lookPathError != nil {
		return "", fmt.Errorf("go/build has no GOROOT and no go is on the path to find it by: %w", lookPathError)
	}
	resolvedExecutable, resolveError := filepath.EvalSymlinks(goExecutable)
	if resolveError != nil {
		return "", fmt.Errorf("resolving the go on the path, %s: %w", goExecutable, resolveError)
	}
	root := filepath.Dir(filepath.Dir(resolvedExecutable))
	return root, checkRoot(
		root,
		"the root of the go on the path ("+goExecutable+", resolved to "+resolvedExecutable+")",
	)
}

// GoRoot returns Find's root, or fails the test naming what was looked at.
func GoRoot(test testing.TB) string {
	test.Helper()
	root, findError := Find()
	if findError != nil {
		test.Fatalf("finding the Go toolchain's root: %v", findError)
	}
	return root
}

// checkRoot refuses a root that holds no go in its bin or no runtime source, saying which, and what the root was
// taken from.
func checkRoot(root string, takenFrom string) error {
	goName := "go"
	if runtime.GOOS == "windows" {
		goName = "go.exe"
	}
	goExecutable := filepath.Join(root, "bin", goName)
	if information, statError := os.Stat(goExecutable); statError != nil || !information.Mode().IsRegular() {
		return fmt.Errorf("%s is %s, which holds no %s", takenFrom, root, goExecutable)
	}
	runtimeSource := filepath.Join(root, "src", "runtime")
	if information, statError := os.Stat(runtimeSource); statError != nil || !information.IsDir() {
		return fmt.Errorf("%s is %s, which holds no directory %s", takenFrom, root, runtimeSource)
	}
	return nil
}
