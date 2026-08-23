package dispatch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrNoToolchain reports that no Go toolchain is available to build with.
var ErrNoToolchain = errors.New("no Go toolchain found on PATH")

// PrebuiltPath is where a shipped binary for this platform lives.
//
// A fresh clone or a CI runner must not need Go, so a binary with the standard ruleset compiled in
// ships alongside the package. It is platform-named because a binary for the wrong architecture is
// worse than a missing one: it fails at exec time with an error about file formats rather than
// about platforms.
func PrebuiltPath(packageDirectory string) string {
	return filepath.Join(packageDirectory, "prebuilt", fmt.Sprintf("verify-%s-%s", runtime.GOOS, runtime.GOARCH))
}

// HasToolchain reports whether a Go toolchain is available.
func HasToolchain() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// ResolvePrebuilt returns the shipped binary for this platform.
//
// It fails loudly and names the platform when there is none. This is the rule the whole package
// exists to hold: the gate this tool replaces printed green over zero files for days because a
// resolver walked up looking for a binary, found none, fell through to a bare command name, and the
// spawn failed into an empty file list — which is indistinguishable from a clean tree. There is no
// fallback here and there must never be one. A missing binary is an error, never a quiet success.
func ResolvePrebuilt(packageDirectory string) (string, error) {
	path := PrebuiltPath(packageDirectory)

	information, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf(
			"no prebuilt verify binary for %s/%s at %s, and no Go toolchain to build one: %w",
			runtime.GOOS, runtime.GOARCH, path, ErrNoToolchain,
		)
	}

	// A binary that is not executable would fail at exec with a permission error that reads like a
	// system problem rather than a packaging one. Naming it here says what is actually wrong.
	if information.Mode()&0o111 == 0 {
		return "", fmt.Errorf("the prebuilt verify binary at %s is not executable", path)
	}

	return path, nil
}
