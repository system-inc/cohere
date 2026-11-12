package main

// runGaps is every way a run fell short of checking everything. Each is said in the footer even when
// the run is green.
type runGaps struct {
	// ProgramFiles is the program's size when the run was narrowed to fewer of its files, zero otherwise.
	ProgramFiles int `json:"programFiles"`
	CrashedFiles int `json:"crashedFiles"`
	// RuleCrashes is each rule that crashed on a file, leaving its verdict there missing. Like a crashed
	// file it fails the run: see runCrash.
	RuleCrashes int `json:"ruleCrashes"`
	// Crashes is each crash the two counts above count, named: the file, the rule for a rule crash, and the
	// cause, a panic in a TypeScript run and the engine's own sentence in an engine's. A count with no reason
	// is a gap nobody can act on, so the list is always as long as the two counts together, and it is the
	// same record `--json` prints as a `crash` line.
	Crashes []runCrash `json:"crashes,omitempty"`
	// RulesSkippingEverything is the rules that declined every file they were offered.
	RulesSkippingEverything int  `json:"rulesSkippingEverything"`
	FormattingNotChecked    bool `json:"formattingNotChecked"`
	NothingToCheck          bool `json:"nothingToCheck"`
	// SourceFiles is how many files of its language the project held before anything kept one out of a check, set
	// only when NothingToCheck is: a TypeScript program's own files, or the files an engine's project record counts
	// in its package. It is the fact a run that checked nothing fails on, and Excluded is only its explanation, so a
	// file kept out for a reason no path counted still fails the run, named as unrecorded (#08cv2mp).
	SourceFiles int `json:"sourceFiles,omitempty"`
	// Excluded is why each file of a run that checked nothing was left out, one entry per reason in the order first
	// met, filled only when NothingToCheck is set. A run whose language's files were all excluded fails, and its
	// footer names each reason and the rule that decided it: a run that checked nothing never reads as a pass
	// (#08cv2mp). A project with no file of its language at all has none, and keeps the warning alone.
	Excluded []excludedReason `json:"excluded,omitempty"`
	// CensusMissed is each file an engine's load reached in its project that its census never counted, with its
	// package (#yyv7nr2, #hd5bsqh), as at a root or a go.work use reached through a symbolic link, where the census
	// once walked nothing. No count of the run accounts for such a file, SourceFiles included, so it fails the run,
	// and the footer says so in place of a project with no file to check.
	CensusMissed []censusMissedFile `json:"censusMissed,omitempty"`
	// IndexFallback is why a format walk read no tracked path from its repository's index (#14skmqy): it decided by
	// the ignore files alone, so a file git tracks under a pattern that matches it went unformatted, counted ignored.
	// A gap, not a failure: which files those are is the index's to say, and it could not be read.
	IndexFallback string `json:"indexFallback,omitempty"`
	// ModifiedBuild is a binary built from a modified tree, which no commit reproduces.
	ModifiedBuild bool `json:"modifiedBuild"`
	// Unread is files the run could not check for any other reason, said as the run said it.
	Unread string `json:"unread,omitempty"`
	// NestedUnread is why the nested repositories could not be read for drift, when they could not. Their
	// files are never this run's to check, so the run goes on, and says so here (#71njcj8).
	NestedUnread string `json:"nestedUnread,omitempty"`
	// PhasesNotRun are the check's phases the run has no record of, so it stopped before them. A run that
	// formatted and never type-checked or linted says it here, rather than reading as 0 rules that found
	// nothing (#71njcj8). Set where a run stops: its failed summary, and a project's run that never finished.
	PhasesNotRun []string `json:"phasesNotRun,omitempty"`
	// AdamicIgnored is the Adamic `.a` files the program holds that git's ignore rules leave out of the
	// format walk: checked, and never formatted (#6mhafvb).
	AdamicIgnored int `json:"adamicIgnored,omitempty"`
	// NestedUnchecked is the files in the nested repositories the run reads that the formatter could not check,
	// each named in the account (#zmt3tv5). A gap and never the verdict: the nested repository is read-only here,
	// and its own run is the one that fails on them.
	NestedUnchecked int `json:"nestedUnchecked,omitempty"`
	// MissingGenerated is each generated file the program asks for that does not exist yet, relative to the
	// project root (#wxsqavy). It fails the run: what the missing file declares was never checked.
	MissingGenerated []string `json:"missingGenerated,omitempty"`
	// FixesHeld is a run whose lint fixes were held because an input the program was told to load is missing
	// (#46ern7e, #4jxsbzr): a generated file, or an import, reference or types entry that resolves to nothing.
	// None was proposed or written, and formatting ran alone.
	FixesHeld bool `json:"fixesHeld,omitempty"`
	// Unchecked is the files an engine's run listed as not checked for a reason other than the ones its language
	// says nobody expects checked (engineLanguage.ExpectedExclusionReasons). In a Swift run that today means no
	// target of the package compiles them and they are no standalone script the run checks alone (#13nbb1s), less
	// those the runs of the Swift projects below it account for (#ybspptb). In a Go run it means a file only another
	// platform builds, or one that imports "C" while cgo is off, since the engine checks the host's build alone; under
	// `--other-builds` a file another build checked is expected, and only one no build the run checked compiles stays
	// (#72n9mq0). A gap, not a failure.
	// Each is named, never only counted: a gap nobody can find is half a gap.
	Unchecked []uncheckedEngineFile `json:"unchecked,omitempty"`
	// NotWritten is the files an engine's fix phase would have written and did not, since each is reached through a
	// symbolic link to a real path outside the root (#hwz114g). Each was checked, its findings stand, and its fixes
	// were withheld, so a fix of this project never writes another's files. A gap that makes the run incomplete, named
	// with where each file really is.
	NotWritten []notWrittenFile `json:"notWritten,omitempty"`
	// ChangedDuringRun is the files whose text changed between the fix phase's walk and its fix, under another agent
	// or an editor's save (#bxmcf34). The walk's fixes for each were refused, since their offsets point into bytes the
	// file no longer holds, and asked for again against the text it holds; its findings describe the text the walk
	// read. A gap that makes the run incomplete, named by file.
	ChangedDuringRun []string `json:"changedDuringRun,omitempty"`
	// WithheldInDependencyCgo is each file of a dependency with cgo files whose type errors a Go run withheld,
	// since no cgo tool ran and the faked package "C" is what they are about, sorted by file as the engine sends
	// them. A gap, not a failure: they are a dependency's, not the project's to fix. Each is named, never only
	// counted, with its package and how many errors it held, so a reader can find what went unsaid.
	WithheldInDependencyCgo []withheldCgoFile `json:"withheldInDependencyCgo,omitempty"`
	// WithheldDependencyCgoDeclarations counts the declarations of dependencies with cgo files that every rule
	// was given without their bodies or initial values, since no cgo tool ran. A gap, not a failure, as the
	// files above are. Counted and not named because the engine counts them and names none: they are spread
	// over whole packages, and the count is what says the rules saw less of those dependencies than all of it.
	WithheldDependencyCgoDeclarations int `json:"withheldDependencyCgoDeclarations,omitempty"`
	// UnguardedWorkspaceSums is each go.work.sum a Go run's go list ran beside with no side copy of its go.work
	// (#34r3v1y): in workspace mode go writes every sum it read that the file lacks, whatever -mod says, and where no
	// copy could answer exactly as the tree's go.work, as beside a vendor directory, go list ran in the tree, so go may
	// have rewritten it. A gap, not a failure: every file was checked. Each is named.
	UnguardedWorkspaceSums []string `json:"unguardedWorkspaceSums,omitempty"`
}

// excludedReason is one reason a run that checked nothing left files out: the words the footer prints, how many
// files it left out, and the first rule that decided, as `path:line` relative to the project root, empty where no
// rule did, as for a narrowing.
type excludedReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Source string `json:"source,omitempty"`
}

// unrecordedReason is what a run that checked nothing says of the files no reason counted. Such a file was kept
// out by a path that never said why, so the words name it as cohere's bug to report rather than the project's.
const unrecordedReason = "for a reason cohere did not record, a cohere bug to report"

// nothingChecked is whether the run had files of its language and checked none of them, which fails it (#08cv2mp).
// It rests on the fact, SourceFiles, and never on a reason having been counted: a path that dropped every file
// without counting one would otherwise read as a project with no file at all, ✓ beside a warning. A reason counted
// of a file the fact leaves out, a Swift file no target compiles, fails it too, as it did before.
func (gaps runGaps) nothingChecked() bool {
	return gaps.NothingToCheck && (gaps.SourceFiles > 0 || len(gaps.Excluded) > 0)
}

// countUnrecorded names, under unrecordedReason, the files of a run that checked nothing that no reason counted:
// SourceFiles less every file Excluded counts. Called once the reasons are in.
func (gaps *runGaps) countUnrecorded() {
	if !gaps.NothingToCheck {
		return
	}
	counted := 0
	for _, excluded := range gaps.Excluded {
		counted += excluded.Count
	}
	if unrecorded := gaps.SourceFiles - counted; unrecorded > 0 {
		gaps.addExcluded(unrecordedReason, "", unrecorded)
	}
}

// addExcluded counts files left out for reason, decided by source, under its reason's entry, keeping the first
// source met for a reason.
func (gaps *runGaps) addExcluded(reason string, source string, files int) {
	for index := range gaps.Excluded {
		if gaps.Excluded[index].Reason == reason {
			gaps.Excluded[index].Count += files
			return
		}
	}
	gaps.Excluded = append(gaps.Excluded, excludedReason{Reason: reason, Count: files, Source: source})
}

// notWrittenSentence is the run's account when every phase ran and its only gap is files whose fixes were not written,
// each reached through a link outside the root. TypeScript's run and an engine's say it in the same words.
const notWrittenSentence = "the notes above name the files whose fixes were not written, each reached through a link " +
	"outside the root"

// changedDuringRunSentence is the run's account when every phase ran and a file changed under it.
const changedDuringRunSentence = "the notes above name the files that changed during the run, whose findings " +
	"describe text they no longer hold"
