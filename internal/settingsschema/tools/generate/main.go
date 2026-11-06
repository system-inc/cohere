// Command generate writes the settings schemas and their reference (internal/settingsschema) into the
// module's schema/ directory. Run it from the module root:
//
//	go run ./internal/settingsschema/tools/generate          write the files
//	go run ./internal/settingsschema/tools/generate -check   exit 1 if any is stale, writing nothing
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/settingsschema"
)

func main() {
	check := flag.Bool("check", false, "report stale files and exit 1, writing nothing")
	flag.Parse()

	files, buildError := settingsschema.Build(registry.Names())
	if buildError != nil {
		exitWith(buildError)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	stale := 0
	for _, path := range paths {
		// Ignored: a file missing or unreadable is not what Build produced, so it is stale, and written afresh
		// unless this is a check.
		existing, _ := os.ReadFile(path)
		if bytes.Equal(existing, files[path]) {
			continue
		}
		stale++
		if *check {
			if _, printError := fmt.Printf("stale: %s\n", path); printError != nil {
				exitWith(fmt.Errorf("naming a stale file: %w", printError))
			}
			continue
		}
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			exitWith(createDirectoryError)
		}
		if writeError := os.WriteFile(path, files[path], 0o644); writeError != nil {
			exitWith(writeError)
		}
		if _, printError := fmt.Printf("wrote %s\n", path); printError != nil {
			exitWith(fmt.Errorf("saying %s was written: %w", path, printError))
		}
	}
	if *check && stale > 0 {
		os.Exit(1)
	}
}

// exitWith says why the run failed, on standard error, and exits 1.
func exitWith(failure error) {
	// Ignored: the exit code below says the run failed even when standard error cannot.
	fmt.Fprintln(os.Stderr, failure)
	os.Exit(1)
}
