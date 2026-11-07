package rename

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/edit"
)

// Apply writes a plan to disk, refusing anything it cannot show to be safe.
//
// Three refusals, in the order they can fire:
//
//	a plan carrying refusals      never written, for any reason. A rename that cannot be done
//	                              correctly is not done at all.
//	overlapping edits             two edits touching the same bytes means the plan is wrong about
//	                              its own sites, and applying either one is a guess.
//	a file that stops parsing     the same guard the fix engine uses, applied here for the same
//	                              reason: a range computed one token off produces plausible-looking
//	                              bytes, and parsing before writing turns that into a refusal
//	                              instead of a corrupted file.
//
// The parse guard is necessary and not sufficient, and this is the one place in the tool where that
// gap is the whole risk rather than a footnote. `internal/rule/rule.go` states it exactly: anything
// sayable in valid syntax about a name that no longer exists slips through, so a rename that
// updates a usage and misses its declaration produces a file that parses and does not compile. That
// is why every correctness question here is answered before this function, from the type graph, and
// why this function's job is only to refuse the mechanical failures.
func Apply(plan *Plan) (int, error) {
	if len(plan.Refusals) > 0 {
		return 0, fmt.Errorf("the plan was refused, so nothing was written")
	}
	if len(plan.Edits) == 0 {
		return 0, nil
	}

	byFile := map[string][]Edit{}
	for _, edit := range plan.Edits {
		byFile[edit.FileName] = append(byFile[edit.FileName], edit)
	}
	if plan.RewriteMentions {
		for _, mention := range plan.Mentions {
			if !mention.Rewritable {
				continue
			}
			byFile[mention.FileName] = append(byFile[mention.FileName], Edit{
				FileName: mention.FileName,
				Range:    mention.Range,
				Line:     mention.Line,
				Column:   mention.Column,
				Text:     plan.NewName,
				Kind:     EditMention,
			})
		}
	}

	// Every file is rewritten in memory and checked before any file is written, so a plan that would
	// corrupt its ninth file does not leave the first eight rewritten. A rename is one act and a
	// partial one is the failure mode this whole verb exists to prevent.
	rewritten := map[string]string{}
	for fileName, edits := range byFile {
		original, readError := os.ReadFile(fileName)
		if readError != nil {
			return 0, fmt.Errorf("reading %s: %w", fileName, readError)
		}
		text := string(original)

		sort.Slice(edits, func(first int, second int) bool {
			return edits[first].Range.Pos() < edits[second].Range.Pos()
		})

		// This guard is subsumed by the `edit.Range.Pos() < previous` bounds check in the apply loop
		// below, and it is kept anyway. Both facts are measured rather than assumed.
		//
		// A mutation disabling this condition survived the suite twice, including after a fixture was
		// written specifically to kill it. The fixture passed both times because the overlapping input
		// is rejected either way: with this guard gone, the second edit's Pos is still less than
		// `previous`, so the loop below refuses it one step later with a different message. Enumerating
		// every ordered pair of ranges over a twelve-position alphabet (20,736 pairs) found zero
		// inputs where the two conditions disagree about accepting or rejecting.
		//
		// So this cannot be pinned by a fixture and never will be, which is why there is no test
		// asserting it and why the next reader should not helpfully add one. It stays because the two
		// checks answer different questions and only this one answers its question at the right time:
		// this refuses the whole file before any rewriting begins and names overlap as the cause, while
		// the bounds check catches the same input midway through building the replacement and reports
		// it as a range outside the file, which is true and points the reader at the wrong problem.
		// A guard that costs one comparison and turns a confusing message into an accurate one earns
		// its place without earning a test.
		for index := 1; index < len(edits); index++ {
			if edits[index].Range.Pos() < edits[index-1].Range.End() {
				return 0, fmt.Errorf(
					"two edits overlap in %s at %d and %d, so the plan is wrong about its own sites",
					fileName, edits[index-1].Range.Pos(), edits[index].Range.Pos(),
				)
			}
		}

		// Applied back to front so an earlier edit never shifts a later edit's offsets. Going
		// forward would require re-deriving every subsequent range against a moving target, which is
		// the class of mistake that writes the right characters into the wrong place.
		var builder strings.Builder
		previous := 0
		for _, edit := range edits {
			if edit.Range.Pos() < previous || edit.Range.End() > len(text) {
				return 0, fmt.Errorf(
					"an edit in %s points outside the file, so the plan does not describe this text",
					fileName,
				)
			}
			builder.WriteString(text[previous:edit.Range.Pos()])
			builder.WriteString(edit.Text)
			previous = edit.Range.End()
		}
		builder.WriteString(text[previous:])
		updated := builder.String()

		// Markdown is prose, with no parse to guard; every source file is parsed, mentions rewritten or not.
		if isMarkdownFile(fileName) {
			rewritten[fileName] = updated
			continue
		}
		if parses, reason := edit.Parses(fileName, updated); !parses {
			return 0, fmt.Errorf(
				"renaming would leave %s unparseable (%s), so nothing was written",
				fileName, reason,
			)
		}
		rewritten[fileName] = updated
	}

	written := 0
	for fileName, text := range rewritten {
		information, statError := os.Stat(fileName)
		mode := os.FileMode(0o644)
		if statError == nil {
			mode = information.Mode()
		}
		if writeError := os.WriteFile(fileName, []byte(text), mode); writeError != nil {
			return written, fmt.Errorf("writing %s: %w", fileName, writeError)
		}
		written++
	}
	return written, nil
}

// Write prints a plan in full: every file, every range, and everything the rename could not see.
//
// Printed before anything is applied, and printed identically whether or not it will be applied.
// The dry run is the same computation as the real run, so a preview cannot drift from what lands.
//
// A bufio.Writer keeps the first failed write's error and hands it back from Flush, so the one check at the end
// catches a plan that never reached out, whichever line failed.
func Write(out io.Writer, plan *Plan, applied bool) error {
	writer := bufio.NewWriter(out)
	if len(plan.Refusals) > 0 {
		fmt.Fprintf(writer, "rename refused: %s\n", plan.OldName)
		for _, refusal := range plan.Refusals {
			fmt.Fprintf(writer, "  %s\n", refusal)
		}
		fmt.Fprintf(writer, "\nNothing was written.\n")
		return flushed(writer, "the refused rename")
	}

	verb := "would rename"
	if applied {
		verb = "renamed"
	}
	// The dash is written as its escape, the same bytes, so the line prints exactly what it always has.
	fmt.Fprintf(writer, "%s %s to %s \u2014 %d sites in %d files\n",
		verb, plan.OldName, plan.NewName, len(plan.Edits), plan.FilesTouched)
	if plan.DeclarationFile != "" {
		fmt.Fprintf(writer, "declared at %s:%d\n", plan.DeclarationFile, plan.DeclarationLine)
	}

	currentFile := ""
	for _, edit := range plan.Edits {
		if edit.FileName != currentFile {
			currentFile = edit.FileName
			fmt.Fprintf(writer, "\n  %s\n", currentFile)
		}
		detail := plan.NewName
		if edit.Kind == EditShorthandExpansion {
			detail = edit.Text
		}
		fmt.Fprintf(writer, "    %d:%d  %s  [%d,%d) -> %s\n",
			edit.Line, edit.Column, edit.Kind, edit.Range.Pos(), edit.Range.End(), detail)
	}

	// The blindness section is printed even when it is empty, and that is deliberate. A section that
	// appears only when there is something to say cannot be distinguished from a tool that forgot to
	// look, and the whole point of this section is that the reader must never be handed a count that
	// implies a completeness the checker cannot deliver.
	fmt.Fprintf(writer, "\nwhat this could not see: ")
	if len(plan.Blind) == 0 {
		fmt.Fprintf(writer, "no string literal in the program spells %q.\n", plan.OldName)
	} else {
		fmt.Fprintf(
			writer,
			"%d string literals spell %q, and a string-keyed use is invisible to the\n",
			len(plan.Blind),
			plan.OldName,
		)
		fmt.Fprintf(
			writer,
			"type checker. If any of these is a property key or a dynamic lookup, this rename did NOT\n",
		)
		fmt.Fprintf(writer, "touch it and the result compiles while being wrong at runtime. Read them:\n")
		for _, blind := range plan.Blind {
			fmt.Fprintf(writer, "    %s:%d:%d  %q\n", blind.FileName, blind.Line, blind.Column, blind.Text)
		}
	}

	writeMentions(writer, plan, applied)

	if !applied {
		fmt.Fprintf(writer, "\nNothing was written. Pass --write to apply.\n")
	}
	return flushed(writer, "the rename plan")
}

// writeMentions prints the text mentions apart from the edits, each with what happens to it. Printed when there
// are none too, for the reason the blindness section is: a section that appears only with something in it cannot
// be told from a rename that never looked.
func writeMentions(writer *bufio.Writer, plan *Plan, applied bool) {
	where := "comments and strings of the files it touches"
	if plan.MarkdownRoot != "" {
		where += ", and the markdown under " + plan.MarkdownRoot
	}
	if len(plan.Mentions) == 0 {
		fmt.Fprintf(writer, "\ntext mentions: none of %s names %s as a whole word.\n", where, plan.OldName)
		return
	}
	rewritable := 0
	for _, mention := range plan.Mentions {
		if mention.Rewritable {
			rewritable++
		}
	}
	fmt.Fprintf(writer, "\ntext mentions: %d in %s name %s as a whole word. A text match is not a proven reference:\n",
		len(plan.Mentions), where, plan.OldName)
	for _, mention := range plan.Mentions {
		fate := "listed only: " + mention.Why
		if mention.Rewritable {
			switch {
			case applied && plan.RewriteMentions:
				fate = "rewritten"
			case plan.RewriteMentions:
				fate = "would be rewritten"
			default:
				fate = "rewritten under --comments"
			}
		}
		fmt.Fprintf(writer, "    %s:%d:%d  %s  %s\n      %s\n", mention.FileName, mention.Line, mention.Column,
			mention.Place, fate, mention.LineText)
	}
	if rewritable > 0 && !plan.RewriteMentions {
		fmt.Fprintf(writer, "  %d of them would be rewritten with --comments, and none is without it.\n", rewritable)
	}
}

// isMarkdownFile reports whether a file is markdown, which a rename writes without a parse.
func isMarkdownFile(fileName string) bool {
	return strings.EqualFold(filepath.Ext(fileName), ".md")
}

// WriteCandidates prints the symbols a bare name resolved to, when it resolved to more than one, buffered as Write is.
func WriteCandidates(out io.Writer, name string, candidates []Candidate) error {
	writer := bufio.NewWriter(out)
	fmt.Fprintf(
		writer,
		"%q names %d different symbols, so this rename is ambiguous and was refused.\n\n",
		name,
		len(candidates),
	)
	for _, candidate := range candidates {
		fmt.Fprintf(writer, "  %s:%d:%d  %s\n", candidate.FileName, candidate.Line, candidate.Column, candidate.Kind)
	}
	fmt.Fprintf(writer, "\nName one of them by position:\n")
	if len(candidates) > 0 {
		fmt.Fprintf(writer, "  cohere rename %s:%d:%d <newName>\n",
			candidates[0].FileName, candidates[0].Line, candidates[0].Column)
	}
	return flushed(writer, "the rename's candidates")
}

// flushed flushes writer, naming what was being written when it or any write before it failed.
func flushed(writer *bufio.Writer, what string) error {
	if flushError := writer.Flush(); flushError != nil {
		return fmt.Errorf("writing %s: %w", what, flushError)
	}
	return nil
}
