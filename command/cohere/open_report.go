package main

import (
	"bufio"
	"fmt"
	"path/filepath"

	"github.com/system-inc/cohere/internal/sentence"
)

// openReport keeps only findings that actually reached the report, after fixes. Children return
// this small record to the discovering parent, which opens the combined set in report order.
type openReport struct {
	Paths   []string
	Checked bool
	// A compiler finding can be global, with no file to open. It still prevents a clean report.
	HasFindings bool
	seen        map[string]bool
}

func (report *openReport) add(path string) {
	if report == nil {
		return
	}
	report.HasFindings = true
	if path == "" {
		return
	}
	resolvedPath := path
	if absolute, absolutePathError := filepath.Abs(path); absolutePathError == nil {
		resolvedPath = absolute
	}
	cleanPath := filepath.Clean(resolvedPath)
	if report.seen == nil {
		report.seen = map[string]bool{}
	}
	if !report.seen[cleanPath] {
		report.seen[cleanPath] = true
		report.Paths = append(report.Paths, cleanPath)
	}
}

func (report *openReport) open(out *bufio.Writer, launch func([]string) error) {
	if len(report.Paths) == 0 {
		if report.Checked && !report.HasFindings {
			fmt.Fprintln(out, "No findings.")
		}
		return
	}
	if launchError := launch(report.Paths); launchError != nil {
		fmt.Fprintf(
			out,
			"cohere: %s\n",
			sentence.Of("could not open editor: "+singleLineDescription(launchError.Error())),
		)
	}
}
