package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if absolute, err := filepath.Abs(path); err == nil {
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
	editor, err := exec.LookPath("code")
	if err != nil {
		return fmt.Errorf("code is not on PATH")
	}
	command := exec.Command(editor, append([]string{"--"}, paths...)...)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	return command.Run()
}

func (report *openReport) open(out io.Writer, launch func([]string) error) {
	if len(report.Paths) == 0 {
		if report.Checked && !report.HasFindings {
			fmt.Fprintln(out, "No findings.")
		}
		return
	}
	if err := launch(report.Paths); err != nil {
		fmt.Fprintf(out, "cohere: could not open editor: %s\n", singleLineDescription(err.Error()))
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
			err = os.WriteFile(path, contents, 0600)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "cohere: returning files to open: %v\n", err)
		}
		return
	}
	report.open(os.Stderr, launchEditor)
}

func readOpenReport(path string) *openReport {
	report := &openReport{}
	contents, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(contents, report) != nil {
			return &openReport{}
		}
	}
	return report
}
