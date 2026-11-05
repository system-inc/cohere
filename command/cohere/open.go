package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
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

const openReportVariable = "COHERE_OPEN_REPORT"

var activeOpen *openReport

func (report *openReport) add(path string) {
	if report == nil {
		return
	}
	report.HasFindings = true
	if path == "" {
		return
	}
	if absolute, absolutePathError := filepath.Abs(path); absolutePathError == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if report.seen == nil {
		report.seen = map[string]bool{}
	}
	if !report.seen[path] {
		report.seen[path] = true
		report.Paths = append(report.Paths, path)
	}
}

// launchEditor is injected into report.open by tests. No shell interprets source file names.
func launchEditor(paths []string) error {
	editor, lookPathError := exec.LookPath("code")
	if lookPathError != nil {
		return fmt.Errorf("code is not on PATH")
	}
	command := exec.Command(editor, append([]string{"--"}, paths...)...)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	// The editor's launcher writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	return command.Run()
}

func (report *openReport) open(out *bufio.Writer, launch func([]string) error) {
	if len(report.Paths) == 0 {
		if report.Checked && !report.HasFindings {
			fmt.Fprintln(out, "No findings.")
		}
		return
	}
	if err := launch(report.Paths); err != nil {
		fmt.Fprintf(out, "cohere: %s\n", sentence.Of("could not open editor: "+singleLineDescription(err.Error())))
	}
}

// finishOpen runs before the dispatcher gets its verdict. An editor failure never changes it.
func finishOpen() {
	report := activeOpen
	if report == nil {
		return
	}
	activeOpen = nil
	if path := os.Getenv(openReportVariable); path != "" {
		contents, err := json.Marshal(report)
		if err == nil {
			err = os.WriteFile(path, contents, 0o600)
		}
		if err != nil {
			fmt.Fprintf(streams.Errors, "cohere: %s\n", sentence.FromError(fmt.Errorf("returning files to open: %w", err)))
		}
		return
	}
	report.open(streams.Errors, launchEditor)
}

func readOpenReport(path string) *openReport {
	report := &openReport{}
	contents, readError := os.ReadFile(path)
	if readError == nil {
		if json.Unmarshal(contents, report) != nil {
			return &openReport{}
		}
	}
	return report
}
