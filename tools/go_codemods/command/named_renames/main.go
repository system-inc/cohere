// Command namedrenames applies renames a person chose, one declaration at a time, where no rule's word casing
// or vocabulary gives the name: `SSAViolation` reads `StaticSingleAssignmentViolation`, the package's own words,
// and not `SsaViolation`. Each line of the -renames file names a declaration by the file that declares it,
// relative to the root, and its old name, then the new name, tab-separated; a line starting with # is a comment.
// An optional fourth field, the line the declaration is on, picks one of two declarations of the name in the
// file (two structs each with a field `err`). A line that names no declaration, or two, proposes nothing and is
// reported. Every proposal goes through the
// global rename engine, which refuses any rename that could change what the program means, and the rewrite is
// reloaded in memory and every identifier's resolution compared before anything is written.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"
)

// pinnedFlags collects repeated -pinned-library values, each a package path, an equals sign and the reason.
type pinnedFlags map[string]string

func (pinned pinnedFlags) String() string { return fmt.Sprint(map[string]string(pinned)) }

func (pinned pinnedFlags) Set(value string) error {
	path, reason, found := strings.Cut(value, "=")
	if !found || path == "" || reason == "" {
		return fmt.Errorf("want a package path, an equals sign and the reason, as path=reason")
	}
	pinned[path] = reason
	return nil
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code namedrenames exits with: runCommandLine returns its exit code here, and
// streams.Finish delivers what was printed and turns a clean exit into 1 when it did not arrive.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine reads the flags and runs namedrenames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print the plan, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	renamesPath := flag.String(
		"renames",
		"",
		"the chosen renames: file, old name, new name, tab-separated, one per line",
	)
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	pinned := pinnedFlags{}
	flag.Var(
		pinned,
		"pinned-library",
		"path=reason: a library package whose exports may be renamed, since every outside consumer reads it at a pin (repeatable)",
	)
	flag.Parse()
	if *renamesPath == "" {
		fmt.Fprintln(streams.Errors, "namedrenames: -renames is required")
		return 2
	}
	chosen, readError := readChosen(*renamesPath)
	if readError != nil {
		fmt.Fprintf(streams.Errors, "namedrenames: %v\n", readError)
		return 1
	}
	return run(*root, skips, chosen, *dryRun, global_rename.Options{PinnedLibraries: pinned})
}

// chosenRename is one line of the renames file.
type chosenRename struct {
	File    string
	OldName string
	NewName string
	// Line is the declaration's line, or 0 when the file declares the name once.
	Line int
}

// readChosen reads the renames file, refusing a line that is not three tab-separated fields and an optional line.
func readChosen(path string) ([]chosenRename, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, openError
	}
	// Ignored: the file was only read, so a failed close loses nothing.
	defer file.Close()
	var chosen []chosenRename
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || len(fields) > 4 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			return nil, fmt.Errorf(
				"%s:%d: want file, old name, new name and an optional line, tab-separated",
				path,
				lineNumber,
			)
		}
		rename := chosenRename{File: fields[0], OldName: fields[1], NewName: fields[2]}
		if len(fields) == 4 {
			declarationLine, lineError := strconv.Atoi(fields[3])
			if lineError != nil || declarationLine < 1 {
				return nil, fmt.Errorf(
					"%s:%d: the fourth field is the declaration's line, got %q",
					path,
					lineNumber,
					fields[3],
				)
			}
			rename.Line = declarationLine
		}
		chosen = append(chosen, rename)
	}
	return chosen, scanner.Err()
}

func run(root string, skips []string, chosen []chosenRename, dryRun bool, options global_rename.Options) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "namedrenames: loading: %v\n", loadError)
		return 1
	}
	fmt.Fprintf(
		streams.Output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(streams.Output)

	proposals, unmatched := findProposals(loaded, chosen)
	fmt.Fprintf(
		streams.Output,
		"chosen: %d lines, %d proposed, %d unmatched\n",
		len(chosen),
		len(proposals),
		len(unmatched),
	)
	for _, line := range unmatched {
		fmt.Fprintf(streams.Output, "  unmatched: %s\n", line)
	}
	result := global_rename.Plan(loaded, proposals, options)
	report(loaded, result)

	if dryRun {
		return 0
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(streams.Output)
	mismatches, applyError := global_rename.Apply(loaded, root, skips, result, streams.Output)
	if applyError != nil {
		fmt.Fprintf(streams.Errors, "namedrenames: %v\n", applyError)
		return 1
	}
	if mismatches > 0 || len(unmatched) > 0 || len(result.Rejections) > 0 {
		return 1
	}
	return 0
}

// findProposals finds, for each chosen line, the one declaration of that name in that file, across every
// analyzed variant; a line naming none, or two declarations (a local and a package-level name alike), is
// unmatched, since the engine renames an object and the line would have to say which.
func findProposals(loaded *workspace.Workspace, chosen []chosenRename) ([]global_rename.Proposal, []string) {
	type declaration struct {
		position token.Position
		proposal global_rename.Proposal
	}
	found := make([]map[string]declaration, len(chosen))
	for index := range chosen {
		found[index] = map[string]declaration{}
	}
	for _, analyzedPackage := range loaded.Analyzed {
		for identifier, object := range analyzedPackage.TypesInfo.Defs {
			if object == nil {
				continue
			}
			position := analyzedPackage.Fset.PositionFor(identifier.Pos(), false)
			relative, inside := loaded.Relative(position.Filename)
			if !inside {
				continue
			}
			for index, line := range chosen {
				if line.File != relative ||
					line.OldName != identifier.Name ||
					line.Line != 0 && line.Line != position.Line {
					continue
				}
				identity := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
				if _, seen := found[index][identity]; !seen {
					found[index][identity] = declaration{
						position: position,
						proposal: global_rename.Proposal{
							Package: analyzedPackage,
							Object:  object,
							NewName: line.NewName,
						},
					}
				}
			}
		}
	}
	var proposals []global_rename.Proposal
	var unmatched []string
	for index, line := range chosen {
		switch len(found[index]) {
		case 1:
			for _, only := range found[index] {
				proposals = append(proposals, only.proposal)
			}
		case 0:
			unmatched = append(
				unmatched,
				fmt.Sprintf("%s %s: no declaration of that name in that file", line.File, line.OldName),
			)
		default:
			var lines []string
			for _, each := range found[index] {
				lines = append(lines, fmt.Sprint(each.position.Line))
			}
			sort.Strings(lines)
			unmatched = append(unmatched, fmt.Sprintf("%s %s: %d declarations of that name in that file, on lines %s",
				line.File, line.OldName, len(found[index]), strings.Join(lines, ", ")))
		}
	}
	return proposals, unmatched
}

// report prints the counts, then every refusal and acceptance.
func report(loaded *workspace.Workspace, result *global_rename.Result) {
	relative := func(position token.Position) string {
		if path, inside := loaded.Relative(position.Filename); inside {
			return fmt.Sprintf("%s:%d", path, position.Line)
		}
		return position.String()
	}
	trim := func(text string) string { return strings.ReplaceAll(text, loaded.Root+"/", "") }
	editFiles := map[string]bool{}
	for _, edit := range result.Edits {
		editFiles[edit.Filename] = true
	}
	fmt.Fprintf(streams.Output, "accepted: %d, refused: %d\n", len(result.Accepted), len(result.Rejections))
	fmt.Fprintf(
		streams.Output,
		"edits: %d identifiers in %d files, %d comment respellings, %d in platform twins outside the load\n",
		len(result.Edits),
		len(editFiles),
		len(result.CommentEdits),
		len(result.TwinEdits),
	)
	fmt.Fprintf(
		streams.Output,
		"model checks: %d unresolved scope walks, %d selection mismatches, %d satisfaction mismatches\n",
		len(result.Unresolved),
		len(result.SelectionModelMismatches),
		len(result.SatisfactionModelMismatches),
	)
	fmt.Fprintln(streams.Output, "\nrefused:")
	for _, rejection := range result.Rejections {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s: [%s] %s\n",
			relative(rejection.Position),
			rejection.Kind,
			rejection.OldName,
			rejection.NewName,
			rejection.Class,
			trim(rejection.Reason),
		)
	}
	fmt.Fprintln(streams.Output, "\naccepted:")
	for _, accepted := range result.Accepted {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s\n",
			relative(accepted.Position),
			accepted.Kind,
			accepted.OldName,
			accepted.NewName,
		)
	}
	fmt.Fprintln(streams.Output, "\nnotes:")
	for _, note := range result.Notes {
		fmt.Fprintln(streams.Output, "  "+trim(note))
	}
}
