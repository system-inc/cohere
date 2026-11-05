// Command notices writes NOTICE and THIRD_PARTY_NOTICES.md at the module root, from cohere's build graph
// and the curated upstreams in internal/release/notices.
//
//	go run ./internal/release/tools/notices
//
// Run it from the module root after a dependency, the compiler pin, the Swift engine's packages or the
// curated list changes. A test in internal/release/notices fails, naming this command, when the committed
// files differ from what it would write.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/system-inc/cohere/internal/release/notices"
)

func main() {
	files, generateError := notices.Generate(".")
	if generateError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintf(os.Stderr, "notices: %v\n", generateError)
		os.Exit(1)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if writeError := os.WriteFile(filepath.Join(".", name), files[name], 0o644); writeError != nil {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(os.Stderr, "notices: writing %s: %v\n", name, writeError)
			os.Exit(1)
		}
		// Ignored: the file is written, which is the command's whole work, and this line only reports it.
		fmt.Printf("wrote %s (%d bytes)\n", name, len(files[name]))
	}
}
