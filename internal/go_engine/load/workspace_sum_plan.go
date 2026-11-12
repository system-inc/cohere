package load

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// workspaceSumPlan is what every side copy of one workspace's go.work is made from (see workspaceSumGuard), read once:
// the go.work go list reads, how go prints its path, and every directory replace go spells from the go.work's
// directory, which a copy elsewhere would have go spell from its own.
type workspaceSumPlan struct {
	// unguarded, when set, says why no side copy can answer exactly as the tree's go.work does, so every command runs
	// in the tree.
	unguarded string

	// workFile is the go.work go list reads, spelled as go list spells it, which the module roots it names are joined
	// to; contents are its bytes and parsed what they say.
	workFile string
	contents []byte
	parsed   *modfile.WorkFile

	// shortWorkFile is workFile as go prints it in an error, base.ShortPath's spelling from the workspace.
	shortWorkFile string

	// moduleReplacements are the relative directory replaces of the go.mod files the go.work uses, each as an absolute
	// directory, which go spells relative to the go.work's directory (modload's canonicalizeReplacePath).
	moduleReplacements []string
}

// readWorkspaceSumPlan reads the plan for the go commands run in workspace under environment, as go env answered
// there (answer), or a plan that names why it cannot guard. A go.work this process cannot read or parse is unguarded
// too: go may read it otherwise, and then write.
func readWorkspaceSumPlan(workspace string, environment []string, answer map[string]string) workspaceSumPlan {
	workFile, doubt := goListWorkFile(workspace, environment, answer)
	if doubt != "" {
		return workspaceSumPlan{unguarded: doubt}
	}
	contents, readError := os.ReadFile(workFile)
	if readError != nil {
		return workspaceSumPlan{unguarded: fmt.Sprintf("reading %s: %v", workFile, readError)}
	}
	parsed, parseError := modfile.ParseWork(workFile, contents, nil)
	if parseError != nil {
		return workspaceSumPlan{unguarded: fmt.Sprintf("parsing %s: %v", workFile, parseError)}
	}
	workDirectory := filepath.Dir(workFile)
	if exists(filepath.Join(workDirectory, "vendor")) {
		// In vendor mode go prints the go.work's own path raw in two errors and short in every other, so no one
		// spelling of the copy's path maps back to the tree's exactly (modload/vendor.go).
		return workspaceSumPlan{unguarded: fmt.Sprintf("%s has a vendor directory beside it", workFile)}
	}
	plan := workspaceSumPlan{
		workFile:      workFile,
		contents:      contents,
		parsed:        parsed,
		shortWorkFile: goShortPath(workspace, workFile),
	}
	for _, use := range parsed.Use {
		moduleRoot := use.Path
		if !filepath.IsAbs(moduleRoot) {
			moduleRoot = filepath.Join(workDirectory, moduleRoot)
		}
		goMod := filepath.Join(moduleRoot, "go.mod")
		moduleContents, moduleReadError := os.ReadFile(goMod)
		if moduleReadError != nil {
			// go refuses the workspace for it before it loads a module, so nothing of it is printed or written.
			continue
		}
		module, moduleParseError := modfile.Parse(goMod, moduleContents, nil)
		if moduleParseError != nil {
			// Refused the same way, before anything is loaded.
			continue
		}
		for _, replace := range module.Replace {
			if replace.New.Version == "" && !filepath.IsAbs(replace.New.Path) {
				plan.moduleReplacements = append(plan.moduleReplacements, filepath.Join(moduleRoot, replace.New.Path))
			}
		}
	}
	return plan
}

// goListWorkFile is the go.work go list reads in workspace under environment, spelled as it spells it, or why that
// cannot be told. go list reads `GOWORK` from the environment, then from the go env file, and with neither it looks
// for go.work from its working directory upward, which `PWD` spells as workspace (modload's FindGoWork). go env was
// asked with this process's own `PWD`, so its answer names the same file, spelled perhaps through another link.
func goListWorkFile(workspace string, environment []string, answer map[string]string) (string, string) {
	if given := environmentValue(environment, "GOWORK"); given != "" && given != "auto" {
		if given != answer["GOWORK"] {
			return "", fmt.Sprintf("GOWORK is %s and go env answered %s", given, answer["GOWORK"])
		}
		return given, ""
	}
	found := findWorkspaceFile(workspace, answer["GOROOT"])
	if found == "" || found == answer["GOWORK"] {
		// Without one found, go env's answer came from the go env file, which go list reads as it is.
		return answer["GOWORK"], ""
	}
	if !sameFile(found, answer["GOWORK"]) {
		return "", fmt.Sprintf("go env answered GOWORK %s where %s is found", answer["GOWORK"], found)
	}
	// Ignored: a go env file nobody can read sets nothing, for go list as for go env, which answered without it.
	settings, _ := os.ReadFile(answer["GOENV"])
	if bytes.Contains(settings, []byte("GOWORK=")) {
		// The go env file may name the same file another way, which go list would read in place of the one found.
		return "", fmt.Sprintf("the go env file %s sets GOWORK", answer["GOENV"])
	}
	return found, ""
}

// findWorkspaceFile is the go.work go finds from directory upward, never crossing goRoot, as modload's
// findWorkspaceFile looks, or empty.
func findWorkspaceFile(directory string, goRoot string) string {
	for current := filepath.Clean(directory); ; {
		candidate := filepath.Join(current, "go.work")
		if information, statError := os.Stat(candidate); statError == nil && !information.IsDir() {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current || parent == goRoot {
			return ""
		}
		current = parent
	}
}

// environmentValue is the value environment gives name, the last one as exec gives it, or empty.
func environmentValue(environment []string, name string) string {
	value := ""
	for _, variable := range environment {
		if given, found := strings.CutPrefix(variable, name+"="); found {
			value = given
		}
	}
	return value
}

// goShortPath is path as go prints it in an error run in workingDirectory, as base.ShortPath shortens it: relative
// when that is shorter and names the same file, or both name none.
func goShortPath(workingDirectory string, path string) string {
	relative, relativeError := filepath.Rel(workingDirectory, path)
	if relativeError != nil || len(relative) >= len(path) {
		return path
	}
	// go stats the relative path from its working directory, unjoined, so a `..` steps to the parent the file system
	// walks to, through a link too.
	if !sameFile(workingDirectory+string(filepath.Separator)+relative, path) {
		return path
	}
	return relative
}

// sameFile is base.ShortPath's sameFile: whether the two name one file, or both name none.
func sameFile(first string, second string) bool {
	firstInformation, firstError := os.Stat(first)
	secondInformation, secondError := os.Stat(second)
	if firstError != nil || secondError != nil {
		return errors.Is(firstError, fs.ErrNotExist) && errors.Is(secondError, fs.ErrNotExist)
	}
	return os.SameFile(firstInformation, secondInformation)
}

// goDirectoryPath is path as go prints a replacement directory, modload's ToDirectoryPath.
func goDirectoryPath(path string) string {
	if modfile.IsDirectoryPath(path) {
		return path
	}
	return "./" + filepath.ToSlash(filepath.Clean(path))
}

// sideContents is the copy of the go.work to write as sideWorkFile, each line where the original's is, and every
// spelling go would print from the copy's place paired with the one it prints from the tree's, or why it cannot be
// made exact. Every relative use is made absolute, as go joins it, so go reads the same module roots. Every relative
// directory replace of the go.work is spelled relative to the copy's directory, a spelling go prints nowhere else.
func (plan workspaceSumPlan) sideContents(workspace string, sideWorkFile string) ([]byte, []spellingPair, string) {
	workDirectory := filepath.Dir(plan.workFile)
	sideDirectory := filepath.Dir(sideWorkFile)
	pairs := []spellingPair{{side: goShortPath(workspace, sideWorkFile), tree: plan.shortWorkFile}}
	var splices []lineSplice
	expectedUses := make([]string, 0, len(plan.parsed.Use))
	for _, use := range plan.parsed.Use {
		path := use.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(workDirectory, path)
			splices = append(splices, splicedLine(use.Syntax, []string{path}, "use"))
		}
		expectedUses = append(expectedUses, path)
	}
	expectedReplacements := make([]string, 0, len(plan.parsed.Replace))
	for _, replace := range plan.parsed.Replace {
		path := replace.New.Path
		if replace.New.Version == "" && !filepath.IsAbs(path) {
			fromSide, relativeError := filepath.Rel(sideDirectory, filepath.Join(workDirectory, path))
			if relativeError != nil || !strings.HasPrefix(fromSide, ".."+string(filepath.Separator)) {
				return nil, nil, fmt.Sprintf("the replacement %s of %s has no spelling from %s", path,
					plan.workFile, sideDirectory)
			}
			pairs = append(pairs, spellingPair{side: fromSide, tree: path})
			tokens := []string{replace.Old.Path}
			if replace.Old.Version != "" {
				tokens = append(tokens, replace.Old.Version)
			}
			splices = append(splices, splicedLine(replace.Syntax, append(tokens, "=>", fromSide), "replace"))
			path = fromSide
		}
		expectedReplacements = append(expectedReplacements, path)
	}
	for _, absolute := range plan.moduleReplacements {
		fromSide, sideError := filepath.Rel(sideDirectory, absolute)
		fromTree, treeError := filepath.Rel(workDirectory, absolute)
		if sideError != nil || treeError != nil {
			return nil, nil, fmt.Sprintf("the replacement %s has no relative spelling", absolute)
		}
		pairs = append(pairs, spellingPair{side: goDirectoryPath(fromSide), tree: goDirectoryPath(fromTree)})
	}
	contents := slices.Clone(plan.contents)
	// From the last line back, so each splice's offsets are still the original's.
	slices.SortFunc(splices, func(first lineSplice, second lineSplice) int { return second.start - first.start })
	for _, splice := range splices {
		contents = slices.Concat(contents[:splice.start], []byte(splice.text), contents[splice.end:])
	}
	if doubt := plan.sameDirectives(sideWorkFile, contents, expectedUses, expectedReplacements); doubt != "" {
		return nil, nil, doubt
	}
	return contents, pairs, ""
}

// sameDirectives says how the copy's contents differ from the go.work's beyond the uses and replacements expected,
// or nothing: every directive on the line it was on, with what it said, so any error go prints of a line names the
// same one.
func (plan workspaceSumPlan) sameDirectives(
	sideWorkFile string,
	contents []byte,
	expectedUses []string,
	expectedReplacements []string,
) string {
	side, parseError := modfile.ParseWork(sideWorkFile, contents, nil)
	if parseError != nil {
		return fmt.Sprintf("the copy of %s does not parse: %v", plan.workFile, parseError)
	}
	differs := len(side.Use) != len(plan.parsed.Use) || len(side.Replace) != len(plan.parsed.Replace) ||
		len(side.Godebug) != len(plan.parsed.Godebug) || len(side.Syntax.Stmt) != len(plan.parsed.Syntax.Stmt)
	for index := 0; !differs && index < len(side.Use); index++ {
		differs = side.Use[index].Path != expectedUses[index] ||
			side.Use[index].Syntax.Start.Line != plan.parsed.Use[index].Syntax.Start.Line
	}
	for index := 0; !differs && index < len(side.Replace); index++ {
		each, original := side.Replace[index], plan.parsed.Replace[index]
		differs = each.Old != original.Old || each.New.Path != expectedReplacements[index] ||
			each.New.Version != original.New.Version || each.Syntax.Start.Line != original.Syntax.Start.Line
	}
	for index := 0; !differs && index < len(side.Godebug); index++ {
		each, original := side.Godebug[index], plan.parsed.Godebug[index]
		differs = each.Key != original.Key || each.Value != original.Value ||
			each.Syntax.Start.Line != original.Syntax.Start.Line
	}
	differs = differs || (side.Go == nil) != (plan.parsed.Go == nil) ||
		(side.Toolchain == nil) != (plan.parsed.Toolchain == nil)
	if !differs && side.Go != nil {
		differs = side.Go.Version != plan.parsed.Go.Version ||
			side.Go.Syntax.Start.Line != plan.parsed.Go.Syntax.Start.Line
	}
	if !differs && side.Toolchain != nil {
		differs = side.Toolchain.Name != plan.parsed.Toolchain.Name ||
			side.Toolchain.Syntax.Start.Line != plan.parsed.Toolchain.Syntax.Start.Line
	}
	if differs {
		return fmt.Sprintf("the copy of %s says something it does not", plan.workFile)
	}
	return ""
}

// splicedLine is the splice that writes tokens, each quoted as go.work quotes it, over the tokens of line, after verb
// on a line outside a block.
func splicedLine(line *modfile.Line, tokens []string, verb string) lineSplice {
	quoted := make([]string, 0, len(tokens)+1)
	if !line.InBlock {
		quoted = append(quoted, verb)
	}
	for _, token := range tokens {
		quoted = append(quoted, modfile.AutoQuote(token))
	}
	return lineSplice{start: line.Start.Byte, end: line.End.Byte, text: strings.Join(quoted, " ")}
}

// lineSplice is the bytes from start to end of one line's tokens, written over with text.
type lineSplice struct {
	start int
	end   int
	text  string
}

// spellingPair is one path as go prints it from the side copy's place and as it prints it from the tree's.
type spellingPair struct {
	side string
	tree string
}
