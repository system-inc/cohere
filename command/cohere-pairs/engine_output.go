package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
)

// engineOutput is what one run's stdout says, as its record keeps it. A count the run never wrote is nil, so a run
// whose project or lint record is missing reads apart from one that counted 0.
type engineOutput struct {
	Findings int `json:"findings"`
	// FindingsMd5 is the md5 of the run's finding lines sorted bytewise, each with its newline, as
	// `grep '"kind":"finding"' | LC_ALL=C sort | md5sum` would print it. The engine's findings never depend on the
	// cache, so every run of one corpus, both sides and modes, has the same.
	FindingsMd5               string          `json:"findingsMd5"`
	FilesInScope              *int            `json:"filesInScope"`
	FilesWalked               *int            `json:"filesWalked"`
	StandardLibraryFromBinary *int            `json:"standardLibraryFromBinary"`
	BuildsAtOnce              int             `json:"buildsAtOnce"`
	BuildsWidthLiveBytes      uint64          `json:"buildsWidthLiveBytes"`
	Builds                    json.RawMessage `json:"builds"`
	// LinesNotJson counts the lines that did not read as JSON, which are counted and never stop the read.
	LinesNotJson int `json:"linesNotJson"`
}

// readEngineOutput reads a run's stdout, one record a line. A run's stream holds one project record and one lint
// record, and should another come, the first of each is the one kept. It fails only when the stream cannot be read.
func readEngineOutput(stream io.Reader) (engineOutput, error) {
	reader := bufio.NewReader(stream)
	read := engineOutput{}
	findings := [][]byte{}
	for {
		// ReadBytes rather than a Scanner, whose line limit a lint record listing every file a build left out passes.
		line, readError := reader.ReadBytes('\n')
		if len(line) > 0 {
			record := bytes.TrimSuffix(line, []byte("\n"))
			parsed := engineLine{}
			if unmarshalError := json.Unmarshal(record, &parsed); unmarshalError != nil {
				read.LinesNotJson++
			} else {
				switch parsed.Kind {
				case "finding":
					findings = append(findings, record)
				case "project":
					if read.FilesInScope == nil {
						read.FilesInScope = &parsed.FilesInScope
					}
				case "lint":
					if read.FilesWalked == nil {
						read.FilesWalked = &parsed.FilesWalked
						read.StandardLibraryFromBinary = &parsed.Cache.StandardLibraryFromBinary
						read.BuildsAtOnce = parsed.BuildsAtOnce
						read.BuildsWidthLiveBytes = parsed.BuildsWidthLiveBytes
						read.Builds = parsed.Builds
					}
				}
			}
		}
		if errors.Is(readError, io.EOF) {
			break
		}
		if readError != nil {
			return engineOutput{}, readError
		}
	}
	read.Findings = len(findings)
	read.FindingsMd5 = sortedLinesMd5(findings)
	return read, nil
}

// sortedLinesMd5 is the hex md5 of lines sorted bytewise, each followed by a newline.
func sortedLinesMd5(lines [][]byte) string {
	sorted := slices.Clone(lines)
	slices.SortFunc(sorted, bytes.Compare)
	hash := md5.New()
	for _, line := range sorted {
		// Ignored: a hash's Write never fails.
		_, _ = hash.Write(line)
		_, _ = hash.Write([]byte("\n"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
