package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/formatfiles"
)

// formatScope is which files the format phase considers, and how that set was decided.
//
// The scope began as a necessity: the goja formatter ran 83 to 100ms per file, so ahra's whole tree
// was minutes against a lint phase of 392ms. The native printers format that tree in about 10
// seconds, so the scope is now about relevance rather than survival. Actual churn on this tree is
// one file per commit and 35 across five, so scoping to changed files puts the common case in
// milliseconds and formats only what the caller touched.
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

	// DependentCount is how many files were added because they import something named, so the
	// coverage line can say why more was checked than was asked for.
	DependentCount int

	// RequestDescription says what the caller asked for, as opposed to what was found. A named
	// directory enumerates every file under it, most of which the program never contained, so the
	// enumerated count is not a number anybody wants beside the count actually checked.
	RequestDescription string

	// UnreadableSubmodules names each submodule git could not be asked about, with why.
	//
	// Carried rather than returned as an error because the two callers want opposite things from
	// it: the format phase can proceed without one directory's worth of scope, and `--changed`
	// cannot, since its scope is the claim the run reports. See changedScopeForCheck.
	UnreadableSubmodules []string
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

	// The scope says how it was decided; this function only adds how much of it is in the program.
	//
	// Four defects of one shape have now passed through these three lines, each a description that
	// was correct where it was written and wrong once another kind of scope started arriving. The
	// last was inferring `named` from a request description being present, which made `--changed`
	// report `7 named` for files nobody named, one heartbeat after that field was added to fix an
	// empty pair of parentheses.
	//
	// So the wording is no longer decided here. The description a constructor already wrote says how
	// its files were chosen, and this function appends to it rather than choosing between spellings
	// it cannot know the caller meant.
	s.Description = fmt.Sprintf("%s, %d of them in the program", s.Description, inPopulation)
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
	names, unreadable, err := gitChangedFiles(workingDirectory, "")
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
		// Named for the coverage line, which asks what the caller requested rather than what was
		// found. Without it a `--changed` run printed `3 in scope ()`, which is a true count beside
		// an empty reason.
		RequestDescription:   "what git reports as changed",
		UnreadableSubmodules: unreadable,
	}, nil
}

// changedScopeForCheck is changedFilesScope for a run that will report a verdict on what it finds.
//
// The format phase can live with a partial answer, because formatting fewer files than changed
// withholds nothing a reader is told was checked. `--changed` cannot. Its scope IS the claim, so a
// submodule whose changes could not be read is a hole in the verdict that reads exactly like a
// submodule with nothing changed, and once an empty change set is a green run that hole is a green
// run over edits nobody looked at. So here it is an error, naming every submodule it could not read.
func changedScopeForCheck(workingDirectory string) (formatScope, error) {
	scope, err := changedFilesScope(workingDirectory)
	if err != nil {
		return formatScope{}, err
	}
	if len(scope.UnreadableSubmodules) > 0 {
		return formatScope{}, fmt.Errorf(
			"could not read what changed in %s, so the change set is incomplete and a verdict over it would not be one",
			strings.Join(scope.UnreadableSubmodules, "; "),
		)
	}
	return scope, nil
}

// gitChangedFiles asks git what has changed, in one call.
//
// `git status --porcelain` covers all three states at once: modified in the working tree, staged,
// and untracked. Three separate commands would be three chances for the sets to disagree if a file
// changed between them, which on a tree with nine writers is not hypothetical.
//
// Names come back relative to workingDirectory, whatever directory of the repository that is. Git
// does not do this on its own: porcelain paths are always relative to the repository's top level,
// whatever directory git ran in, so a project whose root sits below its repository's top had every
// changed path joined onto the wrong prefix and silently matched nothing. That was latent while the
// root had to be the directory cohere ran from; it is reachable now that the root is found by walking
// up, and an empty change set became a green run in the same change, so a wrong prefix would now
// print "nothing changed" over real edits.
//
// since is the commit the caller's repository records for this one, and empty for the outermost
// repository. See the submodule loop for why a submodule needs it.
//
// The second result names each submodule whose changes could not be read, and why. Collected
// rather than failing the call, because what to do about one is the caller's decision: see
// formatScope.UnreadableSubmodules.
func gitChangedFiles(workingDirectory string, since string) ([]string, []string, error) {
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
	//
	// `-- .` keeps the answer to the directory asked about rather than the whole repository, which
	// matters once that directory can be a project below the top level. --ignore-submodules=all is
	// because every submodule is asked directly below, and the parent's own opinion of one adds
	// nothing but a failure mode: a submodule git cannot open fails the parent's call outright, which
	// would name the whole repository as unreadable rather than the one directory that was.
	//
	// The questions this level asks do not depend on each other, so they are asked at once. On ahra the
	// scope made about fourteen git calls in a row across three repositories and was most of a cached
	// run. Errors are read back in the order the calls used to be made, so a failure names the same
	// question it always did, and any failure is still an error rather than an empty answer.
	var (
		output, prefixOutput, head                   string
		statusError, prefixError, headError, listErr error
		submodules                                   map[string]struct{}
		questions                                    sync.WaitGroup
	)
	questions.Add(3)
	go func() {
		defer questions.Done()
		output, statusError = gitOutput(workingDirectory,
			"status", "--porcelain=v1", "--no-renames", "--untracked-files=all", "--ignore-submodules=all", "--", ".")
	}()
	go func() {
		defer questions.Done()
		prefixOutput, prefixError = gitOutput(workingDirectory, "rev-parse", "--show-prefix")
	}()
	go func() {
		defer questions.Done()
		submodules, listErr = listSubmodulePaths(workingDirectory)
	}()
	if since != "" {
		questions.Add(1)
		go func() {
			defer questions.Done()
			head, headError = gitOutput(workingDirectory, "rev-parse", "HEAD")
		}()
	}
	questions.Wait()

	if statusError != nil {
		return nil, nil, fmt.Errorf("asking git what changed: %w", statusError)
	}
	if prefixError != nil {
		return nil, nil, fmt.Errorf("asking git where %s sits in its repository: %w", workingDirectory, prefixError)
	}
	prefix := strings.TrimSpace(prefixOutput)

	changed := parsePorcelain(output)

	// A submodule whose checked-out commit is not the one its parent records has changed files that
	// its own status cannot see, because status compares against the submodule's HEAD and the
	// submodule's HEAD moved with them. Committing inside `libraries/structure` without updating the
	// pointer, or a pull that moved it, leaves every edited file clean to the submodule and invisible
	// to the parent, and an empty change set is now a green run.
	//
	// So the files that differ between the recorded commit and the working tree are added. Deleted
	// files are filtered for the same reason parsePorcelain drops them: there is nothing to read.
	if since != "" {
		if headError != nil {
			return nil, nil, fmt.Errorf("asking git which commit is checked out: %w", headError)
		}
		if strings.TrimSpace(head) != since {
			moved, err := gitOutput(workingDirectory, "diff", "--name-only", "--no-renames", "--diff-filter=d", since, "--", ".")
			if err != nil {
				return nil, nil, fmt.Errorf("asking git what differs from the recorded commit %s: %w", since, err)
			}
			for _, line := range strings.Split(moved, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					changed = append(changed, unquoteGitPath(line))
				}
			}
		}
	}

	// Every path above is relative to the repository's top level; strip that back to this directory.
	// `-- .` guarantees each one starts with the prefix, so one that does not is git saying something
	// this parser does not understand, and that is said rather than guessed at.
	relative := make([]string, 0, len(changed))
	seen := make(map[string]struct{}, len(changed))
	for _, name := range changed {
		if !strings.HasPrefix(name, prefix) {
			return nil, nil, fmt.Errorf("git reported %s, which is outside %s", name, workingDirectory)
		}
		name = strings.TrimPrefix(name, prefix)
		// The status and the recorded-commit diff overlap whenever a file is both uncommitted and
		// behind the recorded commit, and one file counted twice is a count that is wrong.
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		relative = append(relative, name)
	}
	changed = relative

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
	if listErr != nil {
		return nil, nil, listErr
	}
	if len(submodules) == 0 {
		return changed, nil, nil
	}

	kept := make([]string, 0, len(changed))
	for _, name := range changed {
		if _, isSubmodule := submodules[name]; !isSubmodule {
			kept = append(kept, name)
		}
	}

	// Dropping the pointer is only half the answer, and the missing half was silent.
	//
	// A submodule reports as one entry no matter how many files inside it changed, so on this tree
	// an edit to `libraries/structure/source/...` reached here as ` M libraries/structure` and left
	// as nothing at all. Measured: the parent repository sees the edited file zero times and the
	// submodule sees it once.
	//
	// That is the exact shape this layer exists to prevent. A scope computed from the parent alone
	// looks deliberate, runs fast, and never visits the edit, which is worse than being slow.
	//
	// Each submodule is asked with the same flags for the same reasons, and its answers are prefixed
	// back to the parent's path space so every caller downstream keeps receiving paths it can
	// resolve. A submodule that cannot be read does not fail this call, and it is not dropped in
	// silence either: it is named in the second result, and each caller decides whether a partial
	// answer is one it can report on. The first version skipped it without a word, which was
	// tolerable while the only consumer was the formatter and stopped being so once `--changed`
	// started reporting an empty change set as green.
	//
	// A submodule that is not checked out has nothing on disk, so nothing in it can have changed or be
	// in the program; it is passed over rather than asked, since git run inside its empty directory
	// answers for the parent instead.
	//
	// Recursive on purpose, and the first version was not. `libraries/structure` holds `nexus`, so a
	// single level returned that nested pointer as an ordinary path and the formatter reported one
	// file it could not process, which is a true statement about a path that was never a file. The
	// same drop has to happen at every level, which is what calling back into this function does.
	// Each level prefixes only its own segment, so the paths compose rather than doubling.
	//
	// The submodules are asked concurrently. Each answer lands in its own slot and the slots are read
	// back in sorted order, so a gather can drop neither a file nor an error: a lost error would make an
	// incomplete change set look complete, and lost files would make a real edit invisible.
	type submoduleAnswer struct {
		files      []string
		unreadable []string
	}
	names := make([]string, 0, len(submodules))
	for submodule := range submodules {
		names = append(names, submodule)
	}
	sort.Strings(names)
	answers := make([]submoduleAnswer, len(names))
	var asking sync.WaitGroup
	for index, submodule := range names {
		asking.Add(1)
		go func() {
			defer asking.Done()
			directory := filepath.Join(workingDirectory, submodule)
			if _, err := os.Stat(filepath.Join(directory, ".git")); err != nil {
				return
			}
			recorded, err := gitRecordedCommit(workingDirectory, submodule, directory)
			if err != nil {
				answers[index].unreadable = []string{fmt.Sprintf("%s (%v)", submodule, err)}
				return
			}
			inside, insideUnreadable, err := gitChangedFiles(directory, recorded)
			if err != nil {
				answers[index].unreadable = []string{fmt.Sprintf("%s (%v)", submodule, err)}
				return
			}
			for _, name := range inside {
				answers[index].files = append(answers[index].files, filepath.Join(submodule, name))
			}
			for _, description := range insideUnreadable {
				// Concatenated rather than joined: the description carries git's error text, and Join
				// would clean the paths inside it.
				answers[index].unreadable = append(answers[index].unreadable, submodule+string(filepath.Separator)+description)
			}
		}()
	}
	asking.Wait()

	unreadable := []string{}
	for _, answer := range answers {
		kept = append(kept, answer.files...)
		unreadable = append(unreadable, answer.unreadable...)
	}
	sort.Strings(unreadable)

	return kept, unreadable, nil
}

// gitRecordedCommit is the commit the parent's HEAD records for a submodule.
//
// A submodule the parent's HEAD does not hold yet, because it was added since or because the parent
// has no commits, has had every file in it change against HEAD. That is answered with the empty
// tree, so the diff against it lists every tracked file, rather than with an empty string that would
// read as "nothing to compare" and list none.
//
// The empty tree is asked of the submodule rather than written as a constant, because its name
// depends on the repository's hash function.
func gitRecordedCommit(parentDirectory string, submodule string, submoduleDirectory string) (string, error) {
	command := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD:./"+filepath.ToSlash(submodule))
	command.Dir = parentDirectory
	output, err := command.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// --verify --quiet exits 1 with nothing on stderr for a name that does not resolve, and anything
	// else is git failing rather than answering.
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		return "", fmt.Errorf("asking git which commit HEAD records: %w", err)
	}

	emptyTree, err := gitOutput(submoduleDirectory, "hash-object", "-t", "tree", os.DevNull)
	if err != nil {
		return "", fmt.Errorf("asking git for the empty tree: %w", err)
	}
	return strings.TrimSpace(emptyTree), nil
}

// gitOutput runs one git command in a directory and returns what it printed.
//
// Git's own stderr is carried into the error, because `exit status 128` alone names no cause and the
// cause is always on stderr: not a repository, a broken gitdir, an object that was never fetched.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			if message := strings.TrimSpace(string(exitError.Stderr)); message != "" {
				return "", fmt.Errorf("%w: %s", err, message)
			}
		}
		return "", err
	}
	return string(output), nil
}

// gitSubmodulePaths is the set of paths in this repository that are submodules.
//
// Read from the index rather than from .gitmodules, because .gitmodules is a file someone edits and
// the index is what git actually acts on. A submodule is mode 160000 there, a gitlink, which is the
// same fact the formatter needs: this path is not a file to read.
// listSubmodulePaths is how gitChangedFiles asks which paths are submodules. A variable so a test can
// make this one question fail while every other git call succeeds, which no real repository arranges:
// `git ls-files --stage` and `git status` read the same index. Without that, swallowing this error
// passed every test, and a failed listing would silently drop every submodule from the scope.
var listSubmodulePaths = gitSubmodulePaths

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
func (s formatScope) narrowToEnumeration(enumeration formatfiles.Enumeration) formatScope {
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
func describeEnumeration(enumeration formatfiles.Enumeration, formattable int) string {
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

	// A layer that was named and is not there is the broken case a zero hides, so it is said aloud.
	if len(enumeration.MissingLayers) > 0 {
		description += ", ignore file missing at " + strings.Join(enumeration.MissingLayers, ", ")
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
// reconstructed from memory. A project without Structure has two layers instead of three, so the
// layer is named only when libraries/structure exists; a project with Structure and no defaults file
// is the broken case, and the enumeration names it as missing rather than reporting a quiet zero.
// That zero hid a stale path from August, when the file moved, until October.
func resolveStructureIgnorePath(directory string) string {
	root := directory
	if root == "" {
		if workingDirectory, err := os.Getwd(); err == nil {
			root = workingDirectory
		}
	}
	if _, err := os.Stat(filepath.Join(root, "libraries", "structure")); err != nil {
		return ""
	}
	return formatfiles.StructureIgnorePath(root)
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
//
// The whole-tree answer belongs to the project root and not to the working directory, and the two
// stopped being the same directory once the root could be found by walking up. `cohere .` from
// `modules/tasks` means that directory, and reading it as the whole project would turn the
// narrowest thing a caller can type into the widest run there is.
func namedPathsScope(workingDirectory string, root string, names []string) (formatScope, error) {
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
	if root == "" {
		root = workingDirectory
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

		if path == filepath.Clean(root) {
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
