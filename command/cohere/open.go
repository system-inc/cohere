package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

const openReportVariable = "COHERE_OPEN_REPORT"

var activeOpen *openReport

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

// finishOpen runs before the dispatcher gets its verdict. An editor failure never changes it.
func finishOpen() {
	report := activeOpen
	if report == nil {
		return
	}
	activeOpen = nil
	if path := os.Getenv(openReportVariable); path != "" {
		contents, reportError := json.Marshal(report)
		if reportError == nil {
			reportError = os.WriteFile(path, contents, 0o600)
		}
		if reportError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere: %s\n",
				sentence.FromError(fmt.Errorf("returning files to open: %w", reportError)),
			)
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
