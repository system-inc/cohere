package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// formatScope is which files the format phase considers, and how that set was decided.
//
// Formatting the whole tree is not a tuning problem, it is a different tool. Measured: the goja
// formatter runs 83 to 100ms per file with no warm-up, so 3,407 files is 4.7 to 5.7 minutes against
// a lint phase that finishes in 392ms. That is roughly 700 times the rest of the run, and a gate
// nobody will wait for is a gate that does not exist. Actual churn on this tree is one file per
// commit and 35 across five, so scoping to changed files puts the common case in the tens of
// milliseconds.
//
// The set is named as well as computed, because working-tree modifications, staged changes, and a
// diff against a base branch are three different answers to "what changed" and a reader cannot tell
// which one they got from the file count alone.
type formatScope struct {
	// FileNames is the absolute paths in scope, sorted. Nil means every file is in scope.
	FileNames []string

	// index is FileNames as a set, so membership is a map lookup rather than a scan.
	//
	// The transform asks about every project file, so a linear scan here would be quadratic across
	// the tree: 3,407 files against a few hundred changed ones is a million comparisons to answer a
	// question a map answers in one.
	index map[string]struct{}

	// Description says how the set was decided, in words that go straight into the coverage line.
	Description string

	// Everything is whether the scope is the whole tree rather than a subset.
	Everything bool

	// RequestDescription says what the caller asked for, as opposed to what was found. A named
	// directory enumerates every file under it, most of which the program never contained, so the
	// enumerated count is not a number anybody wants beside the count actually checked.
	RequestDescription string
}

// narrowTo reports how many of the scope's files are in a given population, and re-describes the
// scope to say both numbers.
//
// The scope is computed from git and the population is the files the program actually contains, and
// those differ: a changed file can be excluded by the tsconfig or by ignorePatterns and never reach
// the format phase. Reporting only the git number leaves a reader doing arithmetic that does not
// work out, which is what happened when 72 changed files produced 3 formatter candidates and the
// output explained neither.
//
// Both numbers, in one clause, so nobody has to reconstruct the difference.
func (s formatScope) narrowTo(population map[string]struct{}) formatScope {
	if s.Everything {
		return s
	}

	inPopulation := 0
	for _, fileName := range s.FileNames {
		if _, present := population[fileName]; present {
			inPopulation++
		}
	}

	// An empty scope keeps the description changedFilesScope built, because that one names the
	// directory it asked about and this one cannot: narrowing knows the population but not where the
	// question was posed.
	//
	// Overwriting it unconditionally is the bug this branch exists to prevent, and it shipped. The
	// empty-scope naming was correct in changedFilesScope and discarded here two function calls
	// later, so the guard was present, tested, and dead. Its fixture only ever called
	// changedFilesScope directly and never the composition, which is a check that verifies a fragment
	// read as a check on the behavior.
	if len(s.FileNames) == 0 {
		return s
	}

	s.Description = fmt.Sprintf(
		"%d changed files (working tree, staged, and untracked), %d of them in the program",
		len(s.FileNames), inPopulation,
	)
	return s
}

// includes reports whether a file is in a narrow scope.
//
// It deliberately does not consult Everything. That check lives in scopedTransform, which returns
// the unwrapped transform for a whole-tree scope so the filter is never reached at all. Testing it
// in both places looked defensive and was worse than useless: it made the bypass unobservable, so a
// mutant that deleted the fast path survived every fixture because the redundant check produced the
// same answer. Two guards where one is load-bearing means neither can be shown to work.
//
// A whole-tree scope never reaches this method. If one does, it has no index and answers false,
// which is a wrong answer arriving loudly rather than a right answer arriving by accident.
func (s formatScope) includes(fileName string) bool {
	_, inScope := s.index[fileName]
	return inScope
}

// wholeTreeScope is every file, for a deliberate normalization pass or a continuous-integration
// check that wants the whole corpus.
//
// It is a flag rather than a default, and it is allowed to cost five minutes, because someone who
// asks for the whole tree has said what they want. The default costing five minutes is what gets a
// gate bypassed.
func wholeTreeScope() formatScope {
	return formatScope{Everything: true, Description: "every file (--format-all)"}
}

// changedFilesScope resolves the files git reports as changed in the working tree.
//
// Working-tree modifications plus staged changes plus untracked files, which is the set a person
// means by "what I am working on" and the set `s p` formats today. A base-branch diff is a different
// question and would need its own flag rather than silently redefining this one.
//
// Git failing is not a silent fallback to the whole tree. Formatting 3,407 files because a
// subprocess failed would turn a five-minute surprise into the default, so the failure is returned
// and the caller reports the phase as skipped with the reason.
func changedFilesScope(workingDirectory string) (formatScope, error) {
	names, err := gitChangedFiles(workingDirectory)
	if err != nil {
		return formatScope{}, err
	}

	absolute := make([]string, 0, len(names))
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDirectory, name)
		}
		absolute = append(absolute, filepath.Clean(path))
	}
	sort.Strings(absolute)

	index := make(map[string]struct{}, len(absolute))
	for _, path := range absolute {
		index[path] = struct{}{}
	}

	description := fmt.Sprintf("%d changed files (working tree, staged, and untracked)", len(absolute))

	// An empty result is the one answer this function cannot distinguish from a wrong question.
	// "Nothing changed" and "we asked somewhere with nothing to find" print the same line, and the
	// second is silent: git exits zero from any directory inside a repository, so a resolver pointed
	// at the wrong tree reports a clean one rather than failing.
	//
	// So an empty scope names the directory it asked about. That is the existence check beside the
	// silent-empty command, and it costs one clause on a line nobody reads until the day the number
	// is wrong.
	if len(absolute) == 0 {
		description = fmt.Sprintf("0 changed files in %s (working tree, staged, and untracked)", workingDirectory)
	}

	return formatScope{
		FileNames:   absolute,
		index:       index,
		Description: description,
	}, nil
}

// gitChangedFiles asks git what has changed, in one call.
//
// `git status --porcelain` covers all three states at once: modified in the working tree, staged,
// and untracked. Three separate commands would be three chances for the sets to disagree if a file
// changed between them, which on a tree with nine writers is not hypothetical.
func gitChangedFiles(workingDirectory string) ([]string, error) {
	// --untracked-files=all is load-bearing rather than a detail. By default git collapses an
	// untracked directory to a single entry ending in a slash, so `code-quality/` arrives as one
	// "changed file" that matches no real path and every file inside it silently leaves the scope.
	//
	// Measured on the ahra tree: 11 reported entries, three of which were directories hiding 1,958
	// formattable files. Those files would never be formatted, and the coverage line would report
	// them as outside the scope, which is technically true and structurally wrong. With this flag the
	// same tree reports 72 real paths.
	//
	// This is the failure this whole scope layer exists to prevent, arriving through the tool that
	// computes the scope: a subset that looks deliberate and was actually an accident of output
	// formatting.
	command := exec.Command("git", "status", "--porcelain=v1", "--no-renames", "--untracked-files=all")
	command.Dir = workingDirectory

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("asking git what changed: %w", err)
	}

	changed := parsePorcelain(string(output))

	// A modified submodule arrives here looking exactly like a modified file.
	//
	// The trailing-slash guard in parsePorcelain cannot catch it: git reports a submodule whose
	// checked-out commit moved as ` M libraries/structure`, with no slash, because the entry is a
	// gitlink rather than a directory listing. The formatter then tries to read a directory as a
	// file and the run reports a file it could not process, which is a true statement about a
	// path that was never a file.
	//
	// Asked of git rather than answered by stat: a stat call would also reject a path deleted
	// between the status call and the check, which is a different thing and belongs to the caller
	// that reads it.
	submodules, err := gitSubmodulePaths(workingDirectory)
	if err != nil {
		return nil, err
	}
	if len(submodules) == 0 {
		return changed, nil
	}

	kept := make([]string, 0, len(changed))
	for _, name := range changed {
		if _, isSubmodule := submodules[name]; !isSubmodule {
			kept = append(kept, name)
		}
	}
	return kept, nil
}

// gitSubmodulePaths is the set of paths in this repository that are submodules.
//
// Read from the index rather than from .gitmodules, because .gitmodules is a file someone edits and
// the index is what git actually acts on. A submodule is mode 160000 there, a gitlink, which is the
// same fact the formatter needs: this path is not a file to read.
func gitSubmodulePaths(workingDirectory string) (map[string]struct{}, error) {
	command := exec.Command("git", "ls-files", "--stage")
	command.Dir = workingDirectory

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("asking git which paths are submodules: %w", err)
	}

	return parseSubmoduleStage(string(output)), nil
}

// parseSubmoduleStage picks the gitlinks out of `git ls-files --stage` output.
//
// Separated from the subprocess call for the same reason parsePorcelain is: a repository with a
// submodule is awkward to build in a test, and the mode column is the entire decision.
func parseSubmoduleStage(output string) map[string]struct{} {
	submodules := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		// Format is `<mode> <object> <stage>\t<path>`. Mode 160000 is a gitlink.
		if !strings.HasPrefix(line, "160000 ") {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		submodules[unquoteGitPath(line[tab+1:])] = struct{}{}
	}
	return submodules
}

// parsePorcelain turns git's porcelain v1 output into the paths worth formatting.
//
// Separated from the subprocess call so the parsing can be tested against input the subprocess
// cannot easily be made to produce, which is the only way to prove the directory guard does
// anything: the --untracked-files=all flag stops git from ever emitting the line the guard rejects.
func parsePorcelain(output string) []string {
	names := []string{}
	for _, line := range strings.Split(output, "\n") {
		// Porcelain v1 is two status characters, a space, then the path. A line shorter than that is
		// not a record.
		if len(line) < 4 {
			continue
		}
		// A deleted file has nothing to format, and reaching for it would be a read of a path that is
		// gone.
		if line[0] == 'D' || line[1] == 'D' {
			continue
		}
		name := unquoteGitPath(strings.TrimSpace(line[3:]))

		// A trailing slash means git handed back a directory rather than a file, which happens when
		// the untracked-files mode collapses one. The flag above prevents it, and this refuses the
		// entry rather than trusting the flag: a directory in the scope matches no real path, so every
		// file under it leaves the scope without anything saying so.
		if strings.HasSuffix(name, "/") {
			continue
		}

		names = append(names, name)
	}
	return names
}

// unquoteGitPath undoes the quoting git applies to paths with unusual characters.
//
// Git wraps such a path in double quotes and escapes inside it. Left alone, the quotes become part
// of the path and the file silently drops out of scope, which is the shape of a file that is never
// formatted and never reported as skipped.
func unquoteGitPath(path string) string {
	if len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return path
	}
	inner := path[1 : len(path)-1]
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner
}

// scopedTransform narrows a transform to the files a scope admits.
//
// Files outside the scope are skipped with the scope's own description as the reason, so they are
// counted and named rather than quietly passed over. That is the difference between "formatted 12
// files" with no denominator and a line that says what was in scope and why, which is the same
// discipline the skip channel already carries for unhandled file types.
func scopedTransform(inner edit.Transform, scope formatScope) edit.Transform {
	if inner == nil {
		return nil
	}
	if scope.Everything {
		return inner
	}

	return func(fileName string, text string) (string, error) {
		if !scope.includes(fileName) {
			return "", fmt.Errorf("%w: outside the format scope, %s", edit.ErrSkipped, scope.Description)
		}
		return inner(fileName, text)
	}
}

// narrowToEnumeration replaces the type-graph narrowing for the format phase.
//
// The two narrowings answer different questions and the ruling split them. A proposed fix comes from
// a rule that ran over the program, so its candidate must be in the program: `narrowTo` is correct
// for that and stays. A format candidate comes from the disk, and intersecting it with the type
// graph is what made css, markdown, json and yaml invisible, because a tsconfig enumerates
// TypeScript by construction.
//
// The formatter could only speak TypeScript until the goja engine landed, so the type graph was an
// adequate universe by accident. That constraint was incidental and the pipeline mistook it for the
// architecture.
//
// The description states what the walk enumerated rather than what it intended: the root, the count
// before and after the engine's filter, and every declined extension by name. A file the engine
// cannot handle has to be a named zero rather than an absence, which is the one place in this
// pipeline where a file could previously be silently missing.
func (s formatScope) narrowToEnumeration(enumeration prettier.Enumeration) formatScope {
	if s.Everything {
		// A whole-tree scope becomes the enumeration itself: every file the walk found and the engine
		// handles, rather than every file in the type graph.
		index := make(map[string]struct{}, len(enumeration.Files))
		for _, fileName := range enumeration.Files {
			index[fileName] = struct{}{}
		}
		return formatScope{
			FileNames:   enumeration.Files,
			index:       index,
			Description: describeEnumeration(enumeration, len(enumeration.Files)),
		}
	}

	// A narrow scope keeps its own file set, which is the changed set, and reports how much of it the
	// formatter will actually see.
	handled := make(map[string]struct{}, len(enumeration.Files))
	for _, fileName := range enumeration.Files {
		handled[fileName] = struct{}{}
	}

	formattable := 0
	for _, fileName := range s.FileNames {
		if _, present := handled[fileName]; present {
			formattable++
		}
	}

	// An empty scope keeps the description that names where it looked, for the same reason narrowTo
	// keeps it: this function knows the population but not where the question was posed.
	//
	// The Everything guard is not redundant with the branch above. A whole-tree scope also has zero
	// FileNames, so without it a whole-tree scope that reached here would take this early return and
	// come back with Everything still set, and scopedTransform would then bypass filtering entirely
	// and offer the engine files the walk never found. That is a real hole rather than a hypothetical:
	// it is what a mutation deleting the branch above produced, and no fixture could see it because
	// includes is never consulted for a scope that claims to be everything.
	if !s.Everything && len(s.FileNames) == 0 {
		return s
	}
	if s.Everything {
		// Unreachable through the branch above, and stated rather than assumed. A whole-tree scope that
		// arrives here has lost its enumeration, and answering with an empty narrow scope is a loud
		// wrong answer rather than a silent permissive one.
		return formatScope{
			index:       map[string]struct{}{},
			Description: fmt.Sprintf("nothing (a whole-tree scope reached narrowing without its enumeration, walked %s)", enumeration.Root),
		}
	}

	s.Description = fmt.Sprintf(
		"%d changed files (working tree, staged, and untracked), %d the formatter handles · %s",
		len(s.FileNames), formattable, describeEnumeration(enumeration, formattable),
	)
	return s
}

// describeEnumeration renders what the walk actually did.
//
// Stated rather than summarized, because the requirement is that this line cannot become correct by
// accident. When the universe changes, this string has to be rewritten deliberately, so it names the
// root, the ignore layers that removed anything, the nested repositories it refused, and the
// declined extensions.
func describeEnumeration(enumeration prettier.Enumeration, formattable int) string {
	// One format string rather than two. An earlier version branched between a form that named the
	// formattable count and one that did not, and the branch made the root unobservable: a mutation
	// deleting the root from one string left the other correct, so no fixture could see it. Two
	// spellings of one sentence means neither can be shown to work.
	description := fmt.Sprintf(
		"walked %s, %d files, %d the formatter handles",
		enumeration.Root, enumeration.Walked, formattable,
	)

	// Ignore layers are named in the order they ran, and only when they removed something. A layer
	// that removed nothing is either correct or broken, and printing a zero for it every run trains
	// the reader to stop looking.
	layers := make([]string, 0, len(enumeration.IgnoredByLayer))
	for layer := range enumeration.IgnoredByLayer {
		if enumeration.IgnoredByLayer[layer] > 0 {
			layers = append(layers, fmt.Sprintf("%d by %s", enumeration.IgnoredByLayer[layer], layer))
		}
	}
	sort.Strings(layers)
	if len(layers) > 0 {
		description += ", ignored " + strings.Join(layers, ", ")
	}

	// Nested repositories are named rather than counted. "We skipped a repo" is a fact somebody may
	// want to argue with, and a number gives them nothing to argue with.
	if len(enumeration.NestedRepositories) > 0 {
		names := append([]string(nil), enumeration.NestedRepositories...)
		sort.Strings(names)
		description += ", skipped nested repositories " + strings.Join(names, ", ")
	}

	// Declines are the requirement this whole change exists for: a file the engine cannot handle is a
	// named zero rather than an absence.
	if len(enumeration.DeclinedExtensions) > 0 {
		extensions := make([]string, 0, len(enumeration.DeclinedExtensions))
		for extension := range enumeration.DeclinedExtensions {
			extensions = append(extensions, extension)
		}
		sort.Strings(extensions)

		declined := make([]string, 0, len(extensions))
		for _, extension := range extensions {
			declined = append(declined, fmt.Sprintf("%d %s", enumeration.DeclinedExtensions[extension], extension))
		}
		description += ", declined " + strings.Join(declined, ", ")
	}

	return description
}

// resolveStructureIgnorePath locates Structure's shared ignore defaults.
//
// It is one of three ignore layers the walk applies, and the one that is easiest to get wrong:
// @system_cohere_format under-applied it all night and the omission showed up as a 28-file
// discrepancy between two independently built corpora. So it is resolved from a path rather than
// reconstructed from memory, and an absent file is not an error: a project without Structure simply
// has two layers instead of three, and the enumeration reports what each layer removed.
func resolveStructureIgnorePath(directory string) string {
	root := directory
	if root == "" {
		if workingDirectory, err := os.Getwd(); err == nil {
			root = workingDirectory
		}
	}
	return filepath.Join(root, "libraries", "structure", "code-quality", "PrettierIgnoreDefaults.ts")
}

// formatCandidates is the files the format phase should visit.
//
// A whole-tree scope hands back everything the enumeration found; a narrow scope hands back the
// changed set. Either way these are candidates rather than a promise: a file here still passes
// through the transform's own Handles check and can still come back declined, which is what keeps a
// skip visible instead of turning it into an absence.
//
// A scope with no formatter configured yields nothing, so a fix-only run visits exactly the files
// that had proposals and costs what it did before the format phase existed.
func (s formatScope) formatCandidates() []string {
	return s.FileNames
}

// namedPathsScope resolves the paths a caller named on the command line.
//
// A positional path was parsed and discarded before this existed, so `cohere --lint OneFile.ts`
// checked all 3,542 files and reported every finding in the tree. Measured: 3.042s and 5,201
// findings for one named file, against 3.003s and 5,201 for the whole tree. Same cost, same output,
// and no way to tell from either that the argument had been ignored.
//
// A named directory expands to the files under it rather than matching by prefix, so membership
// stays one map lookup for every phase downstream. `.` is the whole tree and says so, which keeps
// `cohere --lint .` meaning what it has always meant instead of quietly becoming a subset of one
// directory entry.
//
// Resolution is against the working directory the caller gave, not the process's own, because
// `--directory` already redefines what a relative path means everywhere else in this binary and a
// second answer here would be a bug nobody could see from the output.
func namedPathsScope(workingDirectory string, names []string) (formatScope, error) {
	// `--directory` defaults to empty, meaning the process's own. Resolving against empty leaves a
	// relative path, which never matches a source file name because those are absolute, so every
	// named path fell out of scope and the run checked nothing while reporting success. That is the
	// green-over-zero-files failure this binary exists to have stopped making, and it was one
	// `filepath.Join` away from shipping.
	if workingDirectory == "" {
		current, err := os.Getwd()
		if err != nil {
			return formatScope{}, fmt.Errorf("resolving the working directory: %w", err)
		}
		workingDirectory = current
	}

	absolute := make([]string, 0, len(names))

	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDirectory, name)
		}
		path = filepath.Clean(path)

		information, err := os.Stat(path)
		if err != nil {
			// A path that does not exist is a loud failure rather than an empty scope. Silently
			// checking nothing because of a typo is the green-over-zero-files defect, and it is the
			// one this binary exists to have stopped making.
			return formatScope{}, fmt.Errorf("resolving %s: %w", name, err)
		}

		if !information.IsDir() {
			absolute = append(absolute, path)
			continue
		}

		if path == filepath.Clean(workingDirectory) {
			return wholeTreeScope(), nil
		}

		err = filepath.WalkDir(path, func(entry string, directoryEntry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if directoryEntry.IsDir() {
				return nil
			}
			absolute = append(absolute, filepath.Clean(entry))
			return nil
		})
		if err != nil {
			return formatScope{}, fmt.Errorf("walking %s: %w", name, err)
		}
	}

	sort.Strings(absolute)

	index := make(map[string]struct{}, len(absolute))
	for _, path := range absolute {
		index[path] = struct{}{}
	}

	description := fmt.Sprintf("%d named path%s", len(absolute), plural(len(absolute)))
	if len(absolute) == 0 {
		description = fmt.Sprintf("0 files under the named paths in %s", workingDirectory)
	}

	request := strings.Join(names, ", ")
	if len(names) > 3 {
		request = fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}

	return formatScope{
		FileNames:          absolute,
		index:              index,
		Description:        description,
		RequestDescription: request,
	}, nil
}

// plural is the one-character suffix that keeps a count reading as English.
func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
