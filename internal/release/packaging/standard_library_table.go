package release

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// The standard library's table a release embeds in every binary that can verify it (task #p1r7v9v). Each target's
// table is generated at release time by a stamped binary of the build machine's own, and compiled in through
// `go build -overlay`, so the committed table.bin, which is empty, is never written.
//
// The versions are every 1.27 patch release and the cgo values both settings, by Kirk's approval of 2026-11-07 for
// about 2.2 MB per target. They are spelled here as the generator's command line takes them, and nowhere else.
const (
	// standardLibraryTableGoVersions are the Go releases each table holds, as `GOTOOLCHAIN` names them.
	standardLibraryTableGoVersions = "go1.27.0,go1.27.1,go1.27.2,go1.27.3"

	// standardLibraryTableCgoValues are the `CGO_ENABLED` values each table holds, under every version.
	standardLibraryTableCgoValues = "1,0"

	// standardLibraryTablePackagePath is the package that embeds table.bin and carries the stamp it is read under.
	standardLibraryTablePackagePath = "github.com/system-inc/cohere/internal/go_engine/standard_library_table"
)

// standardLibraryTableModulePath is table.bin's place in the module, the file a release's overlay replaces.
var standardLibraryTableModulePath = filepath.Join("internal", "go_engine", "standard_library_table", "table.bin")

// targetsWithoutStandardLibraryTable ship the committed empty table, so they compute the standard library as a local
// build does. windows/arm64 is the one, by ruling: no runner here is windows on arm64, so nothing could run its
// `verify-std-facts`, and a table no native binary ever checked is not shipped.
var targetsWithoutStandardLibraryTable = []Target{{GoOperatingSystem: "windows", GoArchitecture: "arm64"}}

// carriesStandardLibraryTable reports whether a release generates a table for target and embeds it.
func carriesStandardLibraryTable(target Target) bool {
	return !slices.Contains(targetsWithoutStandardLibraryTable, target)
}

// standardLibraryReleaseStamp is the stamp every key of a release's table holds: the version, the commit and the Go
// toolchain building it, space separated. One stamp goes on the host binary that generates the tables and on every
// target binary, through releaseLinkerFlags, since a table generated under one stamp reads as nothing under another.
//
// A part that is empty, or holds a quote, is refused: the stamp is quoted inside `-ldflags`, and a quote in it would
// end the quoting partway and stamp something else.
func standardLibraryReleaseStamp(version string, commit string, goToolchain string) (string, error) {
	parts := []string{version, commit, goToolchain}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || strings.ContainsAny(part, `'"`) {
			return "", fmt.Errorf("the standard library's release stamp needs a version, a commit and a Go "+
				"toolchain with no quotes in them, and was given %q", parts)
		}
	}
	return strings.Join(parts, " "), nil
}

// standardLibraryOverlay is the `go build -overlay` file that replaces the module's table.bin with tablePath, as
// JSON.
//
// The key is absolute, as moduleDirectory is made by filepath.Abs, which is the spelling GoBuildCommand gives the go
// command as its working directory too: go matches an overlay by its path as a string, so a key spelled through a
// symbolic link the go command does not see would replace nothing, and the binary would ship the empty table.
func standardLibraryOverlay(moduleDirectory string, tablePath string) ([]byte, error) {
	module, moduleError := filepath.Abs(moduleDirectory)
	if moduleError != nil {
		return nil, fmt.Errorf("resolving the module for the standard library's overlay: %w", moduleError)
	}
	table, tableError := filepath.Abs(tablePath)
	if tableError != nil {
		return nil, fmt.Errorf("resolving the standard library's table for its overlay: %w", tableError)
	}
	overlay := struct {
		Replace map[string]string
	}{Replace: map[string]string{filepath.Join(module, standardLibraryTableModulePath): table}}
	return json.Marshal(overlay)
}

// standardLibraryTableCommand is the command that generates target's table into tablePath, run by hostBinary, a
// stamped binary of the build machine's own. It runs in scratch, outside the module, so nothing it writes lands in
// the source tree.
func standardLibraryTableCommand(hostBinary string, target Target, tablePath string, scratch string) *exec.Cmd {
	command := exec.Command(
		hostBinary,
		"go-engine",
		"standard-library-table",
		"--out", tablePath,
		"--target", target.String(),
		"--versions", standardLibraryTableGoVersions,
		"--cgo", standardLibraryTableCgoValues,
	)
	command.Dir = scratch
	return command
}

// generateStandardLibraryTables builds the stamped host binary into scratch and generates the table of every target
// in targets that carries one, returning each one's overlay file by target. A target that carries none has no entry.
//
// Every table is generated before any target is built, so a toolchain that cannot be fetched fails the release in
// the first minutes and not after the Swift engines. Any failure fails the whole: a release never ships a binary
// whose table was not generated as asked.
func generateStandardLibraryTables(
	options Options,
	targets []Target,
	linkerFlags string,
	scratch string,
) (map[Target]string, error) {
	overlays := map[Target]string{}
	carrying := slices.DeleteFunc(slices.Clone(targets), func(target Target) bool {
		return !carriesStandardLibraryTable(target)
	})
	if len(carrying) == 0 {
		return overlays, nil
	}

	progress := options.Progress
	if progress == nil {
		progress = io.Discard
	}

	// The machine's own, so it runs here; with the targets' linker flags, so its stamp is theirs.
	host := Target{GoOperatingSystem: runtime.GOOS, GoArchitecture: runtime.GOARCH}
	hostBinary := filepath.Join(scratch, "host", host.BinaryFileName())
	if compileError := compile(options.ModuleDirectory, host, linkerFlags, hostBinary, ""); compileError != nil {
		return nil, fmt.Errorf("building the host binary that generates the standard library's tables: %w",
			compileError)
	}

	for _, target := range carrying {
		tablePath := filepath.Join(scratch, target.DirectoryName()+".table.bin")
		// Ignored: the line announces minutes of work, and a failed write to progress changes no table.
		_, _ = fmt.Fprintf(progress, "generating the standard library's table for %s\n", target)
		command := standardLibraryTableCommand(hostBinary, target, tablePath, scratch)
		command.Stdout = progress
		command.Stderr = os.Stderr
		if runError := command.Run(); runError != nil {
			return nil, fmt.Errorf("generating the standard library's table for %s: %w", target, runError)
		}

		if sizeError := reportTableSize(progress, target, tablePath); sizeError != nil {
			return nil, sizeError
		}
		overlay, overlayError := standardLibraryOverlay(options.ModuleDirectory, tablePath)
		if overlayError != nil {
			return nil, overlayError
		}
		overlayPath := filepath.Join(scratch, target.DirectoryName()+".overlay.json")
		if writeError := os.WriteFile(overlayPath, overlay, 0o644); writeError != nil {
			return nil, fmt.Errorf("writing the standard library's overlay for %s: %w", target, writeError)
		}
		overlays[target] = overlayPath
	}
	return overlays, nil
}

// reportTableSize writes the size of target's generated table at tablePath to progress, so every release says what
// each target's table weighs and the next growth is seen where it happens (#pcv6eff): Kirk approved about 2.2 MB per
// target, and the table grew past it when it began carrying each entry's persisted reads.
func reportTableSize(progress io.Writer, target Target, tablePath string) error {
	information, statError := os.Stat(tablePath)
	if statError != nil {
		return fmt.Errorf("reading the size of the standard library's table for %s: %w", target, statError)
	}
	// Ignored: the line reports a size, and a failed write to progress changes no table.
	_, _ = fmt.Fprintf(progress, "the standard library's table for %s is %d bytes\n", target, information.Size())
	return nil
}
