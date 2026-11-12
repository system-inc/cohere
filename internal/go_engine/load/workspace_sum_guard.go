package load

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// workspaceSumGuard runs a workspace's go commands with `GOWORK` at a side copy of its go.work, so a go list that
// rewrites go.work.sum rewrites the copy's and never the tree's (#34r3v1y). In workspace mode go writes every sum it
// read that the file lacks, and drops every one a workspace module's go.sum holds, whatever -mod says: its
// commitRequirements writes go.work.sum with readonly off in workspace mode (modload's
// mustHaveCompleteRequirements), on purpose (golang.org/issue/70272). `go list -m -json all`, the lister's, is the
// one that wrote it on a stale tree; any load can.
//
// The copy names every module root as go joins it, so go reads the same modules, and holds the go.work.sum's bytes.
// What go prints from the copy's place, the work file's own path and each relative directory replace it spells from
// the go.work's directory, is spelled back as go spells it from the tree's, so every answer is go's in the tree byte
// for byte. Where that cannot be made exact, before go runs or because what go printed beside the copy still names the
// copy's place once spelled back, the command runs in the tree as it always did, so its answer is go's own, and the
// guard says go list may have rewritten the sum (unguardedSum), which the load and the run report.
type workspaceSumGuard struct {
	// environment and answer are the whole environment the commands run under and go env's answer under it, which
	// name the go.work in use.
	environment []string
	answer      map[string]string

	// sideRoot is the directory each side copy's own directory is made in, the system's temporary directory when
	// empty. Only a test sets it, to one where nothing can be made, to fail the copy.
	sideRoot string

	// printedAside, when set, is added to what each command printed beside its copy before it is spelled back, given
	// the copy's directory. Only a test sets it, to have go print a spelling of the copy's place nothing maps back.
	printedAside func(sideDirectory string) []byte

	// plans are the plan for each working directory the commands run in, read once each, and unguarded the
	// go.work.sum a command ran beside without a side copy, both under lock.
	lock      sync.Mutex
	plans     map[string]*workspaceSumPlan
	unguarded string
}

// newWorkspaceSumGuard is the guard of the go commands run under environment, as go env answered under it, or nil
// when go reads no go.work, where -mod=readonly, go's default, refuses any write of go.sum.
func newWorkspaceSumGuard(environment []string, answer map[string]string) *workspaceSumGuard {
	if answer["GOWORK"] == "" || answer["GOWORK"] == "off" {
		return nil
	}
	return &workspaceSumGuard{
		environment: environment,
		answer:      answer,
		plans:       map[string]*workspaceSumPlan{},
	}
}

// output runs command, a go command whose output and standard error nothing has taken, and returns what it printed on
// each and how it ended: under a side copy of the go.work when one answers exactly, and otherwise in the tree, noted as
// unguarded. A nil guard runs it as it is.
func (guard *workspaceSumGuard) output(command *exec.Cmd) ([]byte, []byte, error) {
	if guard == nil {
		return runCommand(command)
	}
	environment := command.Env
	if environment == nil {
		// A command that inherits this process's environment gets it whole.
		environment = os.Environ()
	}
	plan := guard.planFor(goWorkingDirectory(command.Dir, environment))
	if plan.unguarded == "" {
		if answer, answered := guard.runAside(command, environment, plan); answered {
			return answer.standardOutput, answer.standardError, answer.runError
		}
	}
	guard.lock.Lock()
	guard.unguarded = guard.answer["GOWORK"] + ".sum"
	guard.lock.Unlock()
	return runCommand(command)
}

// planFor is the plan for commands run in workingDirectory, read on its first command there.
func (guard *workspaceSumGuard) planFor(workingDirectory string) *workspaceSumPlan {
	guard.lock.Lock()
	defer guard.lock.Unlock()
	if plan := guard.plans[workingDirectory]; plan != nil {
		return plan
	}
	plan := readWorkspaceSumPlan(workingDirectory, guard.environment, guard.answer)
	guard.plans[workingDirectory] = &plan
	return &plan
}

// unguardedSum is the go.work.sum a command of the guard ran beside without a side copy, so go list may have rewritten
// it, or empty when every one ran under a copy, or none ran. A nil guard guards nothing go could write.
func (guard *workspaceSumGuard) unguardedSum() string {
	if guard == nil {
		return ""
	}
	guard.lock.Lock()
	defer guard.lock.Unlock()
	return guard.unguarded
}

// goWorkingDirectory is the directory go's os.Getwd names when go runs in directory under environment: its `PWD` as
// given, when that is absolute and names the directory, and otherwise the directory's own path, every link resolved,
// as the system spells the directory a process is in. Every path go shortens in what it prints is relative to it.
func goWorkingDirectory(directory string, environment []string) string {
	runsIn := directory
	if runsIn == "" {
		// Ignored: a process with no working directory runs go nowhere it can shorten a path from.
		runsIn, _ = os.Getwd()
	}
	given := environmentValue(environment, "PWD")
	givenInformation, givenError := os.Stat(given)
	directoryInformation, directoryError := os.Stat(runsIn)
	if filepath.IsAbs(given) && givenError == nil && directoryError == nil &&
		os.SameFile(givenInformation, directoryInformation) {
		return given
	}
	absolute, absoluteError := filepath.Abs(runsIn)
	if absoluteError != nil {
		return runsIn
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		return absolute
	}
	return resolved
}

// runAside runs a copy of command under environment with `GOWORK` at a side copy of the go.work made for it alone, from
// plan, and returns what it printed spelled back as go spells it in the tree, or false when the copy cannot be made, or
// its answer cannot be spelled back exactly: what go printed still names the copy's place once spelled back. The side
// directory holds only the go.work and its go.work.sum, each removed by name once the command ends, a panic included,
// and then the directory.
func (guard *workspaceSumGuard) runAside(
	command *exec.Cmd,
	environment []string,
	plan *workspaceSumPlan,
) (commandOutput, bool) {
	root := guard.sideRoot
	if root == "" {
		root = os.TempDir()
	}
	sideDirectory, makeError := os.MkdirTemp(root, "cohere-go-work-")
	if makeError != nil {
		return commandOutput{}, false
	}
	sideWorkFile := filepath.Join(sideDirectory, "go.work")
	defer removeSide(sideDirectory)
	contents, pairs, doubt := plan.sideContents(sideWorkFile)
	if doubt != "" || !spelledExactly(pairs) {
		return commandOutput{}, false
	}
	if os.WriteFile(sideWorkFile, contents, 0o600) != nil {
		return commandOutput{}, false
	}
	sums, readError := os.ReadFile(plan.workFile + ".sum")
	switch {
	case readError == nil:
		if os.WriteFile(sideWorkFile+".sum", sums, 0o600) != nil {
			return commandOutput{}, false
		}
	case !errors.Is(readError, fs.ErrNotExist):
		return commandOutput{}, false
	}
	aside := exec.Command(command.Args[0], command.Args[1:]...)
	aside.Dir = command.Dir
	// exec keeps the last of a name, so this `GOWORK` is the one go reads.
	aside.Env = append(slices.Clone(environment), "GOWORK="+sideWorkFile)
	standardOutput, standardError, runError := runCommand(aside)
	if guard.printedAside != nil {
		standardOutput = append(standardOutput, guard.printedAside(sideDirectory)...)
	}
	answer := commandOutput{
		standardOutput: spellBack(standardOutput, pairs),
		standardError:  spellBack(standardError, pairs),
		runError:       runError,
	}
	if spelledAside(answer.standardOutput, sideDirectory, pairs) ||
		spelledAside(answer.standardError, sideDirectory, pairs) {
		// Something go printed still names the copy's place, which the tree's run would not.
		return commandOutput{}, false
	}
	return answer, true
}

// commandOutput is what a go command printed on its output and its standard error, and how it ended.
type commandOutput struct {
	standardOutput []byte
	standardError  []byte
	runError       error
}

// removeSide removes the side directory's two files, each by name, and then the directory, which go left nothing
// else in. Any of them gone already is fine, and a directory holding something else is left as it is.
func removeSide(sideDirectory string) {
	for _, name := range []string{"go.work.sum", "go.work"} {
		// Ignored: a file never written is the one case, and nothing is to be done about another.
		_ = os.Remove(filepath.Join(sideDirectory, name))
	}
	// Ignored: a directory holding a file nobody put there stays, never removed with it.
	_ = os.Remove(sideDirectory)
}

// runCommand runs command, taking its output and standard error.
//
// Mutates: command gets its output and standard error, the buffers it returns the bytes of
func runCommand(command *exec.Cmd) ([]byte, []byte, error) {
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	runError := command.Run()
	return standardOutput.Bytes(), standardError.Bytes(), runError
}

// spelledExactly reports whether pairs can be spelled back byte for byte: each side spelling stands for one tree
// spelling, and neither has a character JSON would escape, so one spelled back inside go list's JSON is the string go
// would have written there.
func spelledExactly(pairs []spellingPair) bool {
	trees := map[string]string{}
	for _, pair := range pairs {
		if !jsonPlain(pair.side) || !jsonPlain(pair.tree) || pair.side == "" {
			return false
		}
		if earlier, seen := trees[pair.side]; seen && earlier != pair.tree {
			return false
		}
		trees[pair.side] = pair.tree
	}
	return true
}

// jsonPlain reports whether encoding/json writes text as it is: valid UTF-8 with no quote, backslash, control
// character, and none of the characters it escapes for HTML.
func jsonPlain(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsFunc(text, func(character rune) bool {
		return character < 0x20 || strings.ContainsRune("\"\\<>&\u2028\u2029", character)
	})
}

// spellBack is output with every side spelling of pairs, standing as a whole path, written as its tree spelling. A
// whole path is one with no character of a path just before or after it, so the spelling of one directory is never
// read inside another's, whichever is longer.
func spellBack(output []byte, pairs []spellingPair) []byte {
	present := pairs[:0:0]
	for _, pair := range pairs {
		if pair.side != pair.tree && bytes.Contains(output, []byte(pair.side)) {
			present = append(present, pair)
		}
	}
	if len(present) == 0 {
		return output
	}
	// The longest first, so a spelling that begins another is tried after it.
	slices.SortFunc(present, func(first spellingPair, second spellingPair) int {
		return len(second.side) - len(first.side)
	})
	spelled := make([]byte, 0, len(output))
	for index := 0; index < len(output); {
		matched := false
		if index == 0 || !pathCharacter(output[index-1]) {
			for _, pair := range present {
				if !bytes.HasPrefix(output[index:], []byte(pair.side)) {
					continue
				}
				end := index + len(pair.side)
				if end < len(output) && pathCharacter(output[end]) {
					// A longer path that begins with this spelling, which is not the one spelled.
					continue
				}
				spelled = append(spelled, pair.tree...)
				index = end
				matched = true
				break
			}
		}
		if !matched {
			spelled = append(spelled, output[index])
			index++
		}
	}
	return spelled
}

// pathCharacter reports whether character can stand inside a path go prints, so a spelling beside it is part of a
// longer one. go prints a path between spaces, quotes, parentheses, colons and line ends.
func pathCharacter(character byte) bool {
	return !strings.ContainsRune(" \t\r\n\"'`()[]{},:;", rune(character))
}

// spelledAside reports whether output, spelled back, still names the side copy's place: its directory's own name, or
// a side spelling that stood inside a longer path.
func spelledAside(output []byte, sideDirectory string, pairs []spellingPair) bool {
	if bytes.Contains(output, []byte(filepath.Base(sideDirectory))) {
		return true
	}
	for _, pair := range pairs {
		if pair.side != pair.tree && bytes.Contains(output, []byte(pair.side)) {
			return true
		}
	}
	return false
}
