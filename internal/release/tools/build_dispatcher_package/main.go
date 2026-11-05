// Command build_dispatcher_package writes one npm package to a directory, and compiles nothing.
//
// A release is seven npm packages: six holding a cross-compiled binary each, and the one a consumer
// actually installs, which holds no binary at all. That last one is the dispatcher package — a Node
// entry point that reads the platform, resolves the matching binary package, and execs it. The word
// `build` here is npm's sense and not Go's: this writes a package.json and a script, and there is no
// compilation anywhere in it.
//
// It also does not publish. `command/cohere-release` stages a full release into a directory for a
// person to inspect before anything reaches the registry, and this writes its dispatcher half the
// same way.
//
// The full release takes about two minutes and correctly refuses to stage a partial one when any
// target fails to build. So exercising the INSTALL path — whether `pnpm add` links the right entry
// point, whether `require.resolve` finds the binary package, whether the `os` and `cpu` fields match
// what npm expects — costs two minutes of cross-compilation to test a part with no Go in it.
//
// This writes that one package in a second. It is also what lets the install path be tested while
// `command/cohere` is mid-edit by another author and would not build at all.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/release/packaging"
)

func main() {
	if len(os.Args) < 2 {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintln(os.Stderr, "usage: npm/build_dispatcher_package <output-directory>")
		os.Exit(1)
	}
	directory, runError := run(os.Args[1])
	if runError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
	// Ignored: the package is staged, which is the command's whole work, and this line only reports it.
	fmt.Println("staged", directory)
}

// run stages the dispatcher package under output and returns its directory. A file it cannot write is an
// error, since a package missing a file installs and then fails for whoever runs it.
func run(output string) (string, error) {
	directory := filepath.Join(output, release.UnscopedDispatcherPackageName)
	if createDirectoryError := os.MkdirAll(filepath.Join(directory, "bin"), 0o755); createDirectoryError != nil {
		return "", fmt.Errorf("creating the package directory: %w", createDirectoryError)
	}

	manifest, err := release.DispatcherManifest("0.1.0")
	if err != nil {
		return "", fmt.Errorf("building the package manifest: %w", err)
	}
	if writeError := os.WriteFile(filepath.Join(directory, "package.json"), manifest, 0o644); writeError != nil {
		return "", fmt.Errorf("writing package.json: %w", writeError)
	}

	launcherPath := filepath.Join(directory, "bin", "cohere")
	if writeError := os.WriteFile(launcherPath, []byte(release.DispatcherLauncher()), 0o755); writeError != nil {
		return "", fmt.Errorf("writing the launcher: %w", writeError)
	}
	if changeModeError := os.Chmod(launcherPath, 0o755); changeModeError != nil {
		return "", fmt.Errorf("making the launcher executable: %w", changeModeError)
	}

	// The launcher runs only a binary its checksums list, so they are taken from the platform packages
	// already staged beside this one. With none there it would install and run nothing, so it stops here.
	var platformDirectories []string
	for _, target := range release.Targets {
		if _, statError := os.Stat(filepath.Join(output, target.DirectoryName(), "bin")); statError == nil {
			platformDirectories = append(platformDirectories, target.DirectoryName())
		}
	}
	if len(platformDirectories) == 0 {
		return "", fmt.Errorf(
			"no platform package is staged in %s so the launcher would have no binary it could check; "+
				"stage one there first",
			output,
		)
	}
	checksums, err := release.Checksums(output, platformDirectories)
	if err != nil {
		return "", fmt.Errorf("listing the platform binaries' checksums: %w", err)
	}
	if writeError := os.WriteFile(
		filepath.Join(directory, release.ChecksumsFileName),
		checksums,
		0o644,
	); writeError != nil {
		return "", fmt.Errorf("writing %s: %w", release.ChecksumsFileName, writeError)
	}
	return directory, nil
}
