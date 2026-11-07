// Command format_house rewrites each file it is given in the house format, through native.FormatHouse, the one
// helper every generator in cohere writes through.
//
// Usage:
//
//	go run ./internal/format/native/tools/format_house <file>...
//
// For a generator that cannot call the helper itself because it is not Go, such as
// internal/lint/tools/option_schemas/extract.mjs, which runs this on what it wrote. A Go generator calls
// native.FormatHouse on its bytes before writing them, and never runs this (#4wjhppw).
package main

import (
	"fmt"
	"os"

	"github.com/system-inc/cohere/internal/format/native"
)

func main() {
	if len(os.Args) < 2 {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		fmt.Fprintln(os.Stderr, "format_house: name the files to format")
		os.Exit(2)
	}
	for _, fileName := range os.Args[1:] {
		if formatFileError := formatFile(fileName); formatFileError != nil {
			// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
			fmt.Fprintf(os.Stderr, "format_house: %v\n", formatFileError)
			os.Exit(1)
		}
	}
}

// formatFile rewrites one file in the house format, leaving it untouched when it already is.
func formatFile(fileName string) error {
	text, readError := os.ReadFile(fileName)
	if readError != nil {
		return readError
	}
	formatted, formatHouseError := native.FormatHouse(fileName, text)
	if formatHouseError != nil {
		return formatHouseError
	}
	if string(formatted) == string(text) {
		return nil
	}
	information, statError := os.Stat(fileName)
	if statError != nil {
		return statError
	}
	return os.WriteFile(fileName, formatted, information.Mode().Perm())
}
