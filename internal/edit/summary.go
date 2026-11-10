package edit

import (
	"fmt"
	"sort"
	"strings"
)

// Summary is what a whole fix run did, across every file.
//
// Every field here answers a question a bare "fixed 40 files" cannot. A run that changed forty
// files must not look like a run that changed none, and a run that refused four hundred fixes must
// not look like a run that had none to refuse.
type Summary struct {
	FilesConsidered int
	FilesChanged    int
	FixesApplied    int
	FixesRefused    int

	// FilesTransformSkipped is how many files the transform declined on purpose.
	//
	// Printed alongside the reformat count, because a run where the formatter skipped a whole file
	// type must not read as a run where that file type was already clean.
	FilesTransformSkipped int

	// TransformSkipReasons counts skips by the reason the transform gave, so a reader learns which
	// file types were left alone and why rather than only how many.
	TransformSkipReasons map[string]int

	// FilesTransformed is how many files the whole-text transform rewrote after fixes converged.
	//
	// Counted separately from FilesChanged because the two answer different questions. A run where
	// formatting silently did nothing and a run where nothing needed formatting produce the same
	// FilesChanged, and telling those apart is the same discipline that makes the coverage line
	// worth printing at all.
	FilesTransformed int

	// RefusalsByReason counts refusals by their stated reason, so "we refused four hundred fixes"
	// becomes "three hundred and ninety were overlaps between two rules and ten broke the parse",
	// which is the difference between a number and a finding.
	RefusalsByReason map[string]int

	// FilesByPasses counts files by how many passes they took. A tail here is the signal that some
	// rule proposes one repair where it could propose several, or that two rules are arguing.
	FilesByPasses map[int]int

	// FilesNotConverged names the files that hit the pass ceiling. Named rather than counted,
	// because the answer to "which file needed ten passes" is the only useful next step.
	//
	// A file here was left untouched on disk. Hitting the ceiling discards the run for that file.
	FilesNotConverged []string

	// UnconvergedRules names every rule still proposing when some file hit the ceiling, distinct
	// across the run.
	//
	// This is the actionable half and the file list is not: a rule whose fix does not silence its
	// own finding will do it on every file it matches, so a reader given only filenames goes and
	// reads code that is not the problem.
	UnconvergedRules []string

	// FilesRefused names the files where a pass was discarded because the rewrite did not parse, with the
	// rule that broke each. These are the interesting failures: a rule proposed something that looked fine
	// and was not, which is a bug in that rule's fix.
	FilesRefused []RefusedFile

	// FilesFailed names the files the engine could not process at all: unreadable, unwritable, or
	// already unparseable before anything was applied.
	//
	// Separate from FilesRefused and FilesNotConverged because the three send a reader somewhere
	// different. Refused means a rule proposed something wrong. Not converged means two rules are
	// arguing. Failed means the file never entered the loop, and folding it into either of the
	// others reports a real number under the wrong heading, which reads as a finding about the
	// rules when it is actually a finding about the file.
	FilesFailed []string

	// ChangedDuringRun names every file whose text changed between the walk and the fix, sorted: its walk's
	// proposals were refused and its fixes asked for again against the text in hand (#bxmcf34).
	ChangedDuringRun []string

	// NotWritten names every file a run that writes left as it was because its real path is outside the
	// run's root, with that path, sorted by name. Each was checked as any other file and its fixes are
	// withheld, so the run is incomplete and says where (#td3zta2).
	NotWritten []NotWrittenFile

	// ChangedFiles names every file counted in FilesChanged, with what changed it, sorted by name.
	//
	// The count says how much; this says where and why, which is what `--no-fix` reports as findings:
	// a file that would change is a finding against that file, and the reader needs the rule or the
	// formatter that would change it to know whether to run the fixer or read the code.
	ChangedFiles []ChangedFile

	// Checked is set when the run computed what it would write and wrote nothing, so the summary line
	// says "would be rewritten" rather than claiming a rewrite that never happened.
	Checked bool

	// NotTransformed names every file the transform declined or failed on, with its reason, sorted by
	// name. The counts above say how many; a caller whose verdict is the transform alone needs to know
	// which, since a file the formatter could not read is a file nobody checked.
	NotTransformed []NotTransformedFile
}

// Refusals is a line for each file whose rewrite did not parse: the file, the rule to look at, and the
// parser's message. Printed under the summary line, which counts them, so the count can be traced.
func (summary Summary) Refusals() []string {
	lines := make([]string, 0, len(summary.FilesRefused))
	for _, refused := range summary.FilesRefused {
		blame := fmt.Sprintf("the fix from %s breaks it", strings.Join(refused.Breaking, " and from "))
		switch {
		case len(refused.Rules) == 1 && refused.Rules[0] == transformRuleName:
			blame = "the formatter's output breaks it"
		case len(refused.Breaking) == 0:
			blame = fmt.Sprintf(
				"no one fix breaks it alone, the fixes from %s together do",
				strings.Join(refused.Rules, ", "),
			)
		}
		lines = append(
			lines,
			fmt.Sprintf("refused: %s: %s; nothing was written to it. %s", refused.FileName, refused.Reason, blame),
		)
	}
	return lines
}

// String renders the summary line that follows a fix run.
//
// It always prints, and it always prints the population alongside the result. The gate this tool
// replaces printed a green checkmark over zero files linted for days, because an empty file list is
// indistinguishable from a clean tree. The same ambiguity is available to a fixer, and this is what
// closes it.
func (summary Summary) String() string {
	// A checked run states the same numbers in the conditional, because "3 files rewritten" over a tree
	// nobody wrote to is a count claiming work that never happened.
	rewritten, applied, reformatted := "rewritten", "applied", "reformatted"
	if summary.Checked {
		rewritten, applied, reformatted = "would be rewritten", "would apply", "would be reformatted"
	}
	line := fmt.Sprintf(
		"fix: %d of %d files %s, %d fixes %s, %d refused",
		summary.FilesChanged, summary.FilesConsidered, rewritten, summary.FixesApplied, applied, summary.FixesRefused,
	)

	// The refusal breakdown attaches to the refusal count and must stay adjacent to it. It was
	// written when nothing sat between the two, and adding the reformat and skip clauses moved it
	// away, producing "2 not formatted (2 markdown) (1 overlaps another fix)", where
	// the second parenthetical reads as a second skip reason. A true number under the wrong heading,
	// found by reading the rendered line rather than by any assertion, because every fixture checked
	// for the presence of its own substring and none checked what the whole line said.
	if len(summary.RefusalsByReason) > 0 {
		reasons := make([]string, 0, len(summary.RefusalsByReason))
		for reason := range summary.RefusalsByReason {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)

		line += " ("
		for index, reason := range reasons {
			if index > 0 {
				line += ", "
			}
			line += fmt.Sprintf("%d %s", summary.RefusalsByReason[reason], reason)
		}
		line += ")"
	}

	// Formatting reports its own number rather than hiding inside the rewrite count, so a run where
	// the formatter did nothing is distinguishable from one where nothing needed formatting.
	if summary.FilesTransformed > 0 {
		line += fmt.Sprintf(", %d %s", summary.FilesTransformed, reformatted)
	}

	// Skips print whenever there are any, and they name the reason. A formatter that declined four
	// hundred files and one that found four hundred files already correct produce the same reformat
	// count, and only this line tells them apart.
	if summary.FilesTransformSkipped > 0 {
		line += fmt.Sprintf(", %d not formatted", summary.FilesTransformSkipped)
		reasons := make([]string, 0, len(summary.TransformSkipReasons))
		for reason := range summary.TransformSkipReasons {
			if reason != "" {
				reasons = append(reasons, reason)
			}
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			line += fmt.Sprintf(" (%d %s)", summary.TransformSkipReasons[reason], reason)
		}
	}

	maximumPasses := 0
	for passes := range summary.FilesByPasses {
		if passes > maximumPasses {
			maximumPasses = passes
		}
	}
	if maximumPasses > 1 {
		line += fmt.Sprintf(", up to %d passes", maximumPasses)
	}

	if len(summary.FilesNotConverged) > 0 {
		line += fmt.Sprintf(", %d files did not converge", len(summary.FilesNotConverged))
	}

	// A file that changed under the run had its walk's fixes refused and asked for again, so its findings
	// describe bytes that are gone. Said, never folded into the refusal count alone.
	if len(summary.ChangedDuringRun) > 0 {
		line += fmt.Sprintf(", %d changed during the run (fixes measured again)", len(summary.ChangedDuringRun))
	}

	// A file the run would have written and did not, since it lies outside the root through a link, is
	// said here as well as in the footer's gap, so the fix line never reads as every rewrite landing.
	if len(summary.NotWritten) > 0 {
		line += fmt.Sprintf(", %d not written (reached through a link outside the root)", len(summary.NotWritten))
	}

	// Failures print even though they were already written to stderr as they happened. A summary
	// that omits them reads as a clean run to anyone reading only the last line, which is most
	// readers most of the time.
	if len(summary.FilesFailed) > 0 {
		line += fmt.Sprintf(", %d files could not be processed", len(summary.FilesFailed))
	}

	return line
}
