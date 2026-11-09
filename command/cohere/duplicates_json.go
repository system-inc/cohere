package main

import (
	"bufio"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// The duplicates report under `--json`, before the summary line: a `duplicate` line per group, a `duplicateRefused`
// line per refused function, then one `duplicateCoverage` line, which a requested report always writes, so a reader
// never takes its absence for no groups. None is a finding, and none moves the summary's counts or verdict.
// schema/CohereOutput.schema.json describes them.

// duplicateJson is one group.
type duplicateJson struct {
	Kind    string                `json:"kind"`
	Size    int                   `json:"size"`
	Digest  string                `json:"digest"`
	Members []duplicateMemberJson `json:"members"`
}

// duplicateMemberJson is one member of a group. Kept is the reason a suppression keeps it in its shape.
type duplicateMemberJson struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Name string `json:"name"`
	Kept string `json:"kept,omitempty"`
}

// duplicateRefusedJson is one function the hash refused.
type duplicateRefusedJson struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// duplicatePlaceJson is a line of a file.
type duplicatePlaceJson struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// duplicateCrashJson is a file the hash could not read at all.
type duplicateCrashJson struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// duplicateCoverageJson is what the report looked at, and what it cost.
type duplicateCoverageJson struct {
	Kind             string                                    `json:"kind"`
	Ran              bool                                      `json:"ran"`
	NotRun           string                                    `json:"notRun,omitempty"`
	Floor            int                                       `json:"floor"`
	Files            int                                       `json:"files"`
	FilesIgnored     int                                       `json:"filesIgnored"`
	Functions        int                                       `json:"functions"`
	Hashed           int                                       `json:"hashed"`
	Refused          map[duplicate_functions.RefusalReason]int `json:"refused"`
	NotRead          map[duplicate_functions.NotReadReason]int `json:"notRead"`
	Groups           int                                       `json:"groups"`
	Withheld         int                                       `json:"withheld"`
	BelowFloor       int                                       `json:"belowFloor"`
	DeadSuppressions []duplicatePlaceJson                      `json:"deadSuppressions"`
	Crashes          []duplicateCrashJson                      `json:"crashes"`
	Seconds          float64                                   `json:"seconds"`
	Cache            duplicate_functions.CacheUse              `json:"cache"`
}

// writeDuplicatesJson writes the report's lines.
func writeDuplicatesJson(out *bufio.Writer, report duplicate_functions.Report) {
	for _, group := range report.Groups {
		line := duplicateJson{Kind: "duplicate", Size: group.Size, Digest: group.Digest.String(),
			Members: make([]duplicateMemberJson, 0, len(group.Members))}
		for _, member := range group.Members {
			line.Members = append(line.Members, duplicateMemberJson{Path: member.File, Line: member.Line,
				Name: member.Name, Kept: member.Kept})
		}
		writeJsonLine(out, line)
	}
	coverage := duplicateCoverageJson{
		Kind:             "duplicateCoverage",
		Ran:              report.Ran,
		NotRun:           report.NotRun,
		Floor:            report.Floor,
		Files:            report.Files,
		FilesIgnored:     report.FilesIgnored,
		Functions:        report.Functions,
		Hashed:           report.Hashed,
		Refused:          map[duplicate_functions.RefusalReason]int{},
		NotRead:          map[duplicate_functions.NotReadReason]int{},
		Groups:           len(report.Groups),
		Withheld:         report.Withheld,
		BelowFloor:       report.BelowFloor,
		DeadSuppressions: []duplicatePlaceJson{},
		Crashes:          []duplicateCrashJson{},
		Seconds:          float64(report.ElapsedMilliseconds) / 1000,
		Cache:            report.Cache,
	}
	for _, refused := range report.Refused {
		writeJsonLine(out, duplicateRefusedJson{Kind: "duplicateRefused", Path: refused.File, Line: refused.Line,
			Name: refused.Name, Reason: string(refused.Reason), Detail: refused.Detail})
		coverage.Refused[refused.Reason]++
	}
	for reason, count := range report.NotRead {
		coverage.NotRead[reason] = count
	}
	for _, dead := range report.DeadSuppressions {
		coverage.DeadSuppressions = append(coverage.DeadSuppressions, duplicatePlaceJson{Path: dead.File,
			Line: dead.Line})
	}
	for _, crash := range report.Crashes {
		coverage.Crashes = append(coverage.Crashes, duplicateCrashJson{Path: crash.File, Error: crash.Error})
	}
	writeJsonLine(out, coverage)
}
