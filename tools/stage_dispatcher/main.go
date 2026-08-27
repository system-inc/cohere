// Command stage_dispatcher stages only the dispatcher package, which compiles nothing.
//
// It exists so the install path can be exercised while `command/verify` is mid-edit by another author:
// the full release build cross-compiles the real binary and correctly refuses to stage a partial
// release when that build fails.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/system-inc/verify/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: stage_dispatcher <output-directory>")
		os.Exit(1)
	}

	directory := filepath.Join(os.Args[1], release.DispatcherPackageName)
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o755); err != nil {
		panic(err)
	}

	manifest, err := release.DispatcherManifest("0.1.0")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), manifest, 0o644); err != nil {
		panic(err)
	}

	launcherPath := filepath.Join(directory, "bin", "verify")
	if err := os.WriteFile(launcherPath, []byte(release.DispatcherLauncher()), 0o755); err != nil {
		panic(err)
	}
	if err := os.Chmod(launcherPath, 0o755); err != nil {
		panic(err)
	}

	fmt.Println("staged", directory)
}
