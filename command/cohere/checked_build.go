package main

import "fmt"

// checkedBuild is one build a Go run checked when it checked more than one, as the lint record's `builds` says it
// (swift/Contract.md, internal/go_engine/contract.BuildRecord), the host's first: its target as go env would make it,
// with ` cgo` when cgo is on and its tags after ` tags `; the files of ours it compiles and the packages it analyzed,
// every package of the workspace in each; and the findings that stand which it kept and the host's did not, and those
// it kept that the merge left out. A run of one build writes none, and neither does a Swift run, so their output is
// as it was.
type checkedBuild struct {
	Target           string `json:"target"`
	Host             bool   `json:"host"`
	CompiledFiles    int    `json:"compiledFiles"`
	AnalyzedPackages int    `json:"analyzedPackages"`
	FindingsAdded    int    `json:"findingsAdded"`
	FindingsDropped  int    `json:"findingsDropped"`
	// FilesAlone, NotLinted and NotLintedReason are the files of ours only this build compiled and linted, and those it
	// compiled that no rule ran on, with why (#ytnvt8k), each absent when there is nothing to count.
	FilesAlone      int      `json:"filesAlone,omitempty"`
	NotLinted       []string `json:"notLinted,omitempty"`
	NotLintedReason string   `json:"notLintedReason,omitempty"`
}

// line is the build's clause of the --verbose account, after "lint: ". The host's says what it covered, and the
// findings the merge dropped of it only when there were any, since it adds none by definition; another's says what it
// changed of the verdict, which is the point of checking it.
func (build checkedBuild) line() string {
	covered := counted(build.CompiledFiles, "file compiled", "files compiled") + ", " +
		counted(build.AnalyzedPackages, "package analyzed", "packages analyzed")
	if build.Host {
		line := "the host's build, " + build.Target + ": " + covered
		if build.FindingsDropped > 0 {
			line += ", " + counted(build.FindingsDropped, "finding", "findings") + " dropped by the merge"
		}
		return line
	}
	line := fmt.Sprintf("build %s: %s, %s, %s dropped by the merge", build.Target, covered,
		counted(build.FindingsAdded, "finding added", "findings added"), grouped(build.FindingsDropped))
	if build.FilesAlone > 0 {
		line += ", " + counted(build.FilesAlone, "file", "files") + " no other build compiles"
	}
	if len(build.NotLinted) > 0 {
		line += ", " + counted(len(build.NotLinted), "file", "files") + " compiled and not linted"
	}
	return line
}
