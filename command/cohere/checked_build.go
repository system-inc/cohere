package main

import "fmt"

// checkedBuild is one build a Go run checked when it checked more than one, as the lint record's `builds` says it
// (swift/Contract.md, internal/go_engine/contract.BuildRecord), the host's first: its target as go env would make it,
// with ` cgo` when cgo is on and its tags after ` tags `; the files of ours it compiles and the packages it analyzed,
// every package of the workspace in each; and the findings that stand which it kept and the host's did not, and those
// it kept that the merge left out. Then what the build restored from the host's run rather than checked, each count
// written only when not 0: the host's entries kept for the others, and another's packages restored, declined by its
// load and by its check, every decline by why, the loads again it took, by why, and the packages it wrote through to
// its own findings cache. A run of one build writes none, and neither does a Swift run, so their output is as it was.
type checkedBuild struct {
	Target           string         `json:"target"`
	Host             bool           `json:"host"`
	CompiledFiles    int            `json:"compiledFiles"`
	AnalyzedPackages int            `json:"analyzedPackages"`
	FindingsAdded    int            `json:"findingsAdded"`
	FindingsDropped  int            `json:"findingsDropped"`
	EntriesKept      int            `json:"entriesKept,omitempty"`
	RestoredFromHost int            `json:"restoredFromHost,omitempty"`
	DeclinedAtLoad   int            `json:"declinedAtLoad,omitempty"`
	DeclinedAtCheck  int            `json:"declinedAtCheck,omitempty"`
	DeclinedBy       map[string]int `json:"declinedBy,omitempty"`
	ReloadRounds     int            `json:"reloadRounds,omitempty"`
	ReloadRoundsBy   map[string]int `json:"reloadRoundsBy,omitempty"`
	// WrittenThrough is the packages it restored from the host's run whose entry it wrote to its own findings cache,
	// so its next run hits them there, absent when 0.
	WrittenThrough int `json:"writtenThrough,omitempty"`
	// LoadMilliseconds, LintMilliseconds, DecideMilliseconds (summed across goroutines), KeepMilliseconds and
	// WaitMilliseconds (the host's) are where the build's time went, each absent when 0.
	LoadMilliseconds   int `json:"loadMilliseconds,omitempty"`
	LintMilliseconds   int `json:"lintMilliseconds,omitempty"`
	DecideMilliseconds int `json:"decideMilliseconds,omitempty"`
	KeepMilliseconds   int `json:"keepMilliseconds,omitempty"`
	WaitMilliseconds   int `json:"waitMilliseconds,omitempty"`
	// CheckedFromSource is the packages another build checked from source over every load, a reload's included, and
	// CheckedFromSourceRounds each load's count in order, written only when it was loaded more than once (#k8gty9e).
	// PackagesLoaded is every package its first load loaded, whose share checked from source weighs its lint, and
	// LintWaitMilliseconds and LintWaited how long and whether that lint waited for its weight (#b2t9xmf), each absent
	// when 0 or false, and the host's never written.
	CheckedFromSource       int   `json:"checkedFromSource,omitempty"`
	CheckedFromSourceRounds []int `json:"checkedFromSourceRounds,omitempty"`
	PackagesLoaded          int   `json:"packagesLoaded,omitempty"`
	LintWaitMilliseconds    int   `json:"lintWaitMilliseconds,omitempty"`
	LintWaited              bool  `json:"lintWaited,omitempty"`
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
