package go_root

import (
	"go/build"
	"os"
	"os/exec"
	"slices"
)

// clangPlatforms are the platforms whose default C compiler is clang, cmd/dist's clangos; every other's is gcc.
var clangPlatforms = []string{"darwin", "ios", "freebsd", "openbsd"}

// CCompiler is the C compiler go env's `CC` names, in this process: `CC` when it is set, or else the one a toolchain
// built without `CC` runs, clang where cmd/dist's clangos says and gcc elsewhere. A toolchain built with `CC` set runs
// that one instead (Homebrew's names cc), and the go env file is not read: a machine's that set `CC` there would
// differ.
func CCompiler() string {
	if named := os.Getenv("CC"); named != "" {
		return named
	}
	if slices.Contains(clangPlatforms, build.Default.GOOS) {
		return "clang"
	}
	return "gcc"
}

// CgoEnabled is go env's `CGO_ENABLED`, in this process, decided as cmd/go decides it: `CGO_ENABLED` when it is 0 or
// 1, or else go/build.Default's choice for the platform, turned off where `CC` is unset and the default C compiler is
// on no directory of the path, as go does since Go 1.20 so a machine with no C compiler builds. The go env file is not
// read, as CCompiler says.
func CgoEnabled() bool {
	switch os.Getenv("CGO_ENABLED") {
	case "1":
		return true
	case "0":
		return false
	}
	// go/build.Default decides off for a platform without cgo and for a build that targets another platform than the
	// binary's own, where go looks for no compiler.
	if !build.Default.CgoEnabled {
		return false
	}
	if os.Getenv("CC") != "" {
		return true
	}
	_, lookPathError := exec.LookPath(CCompiler())
	return lookPathError == nil
}
