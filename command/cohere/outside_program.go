package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// programExtensions are the extensions a tsconfig can put into a program at all. Anything else, a
// markdown file or a JSON file, is outside every program by construction, whatever the config says.
var programExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
}

// errNamedPathsOutsideProgram is the refusal for a named-path run that reached no program file,
// carrying explainNamedPathsOutsideProgram's reason.
func errNamedPathsOutsideProgram(location projectLocation, names []string) error {
	return errors.New(explainNamedPathsOutsideProgram(location, names))
}

// explainNamedPathsOutsideProgram says why none of the paths named on the command line reached the
// program, one sentence per path.
//
// The walk refuses an empty file set, which is right, and used to say only "nothing to walk: the
// file set is empty". Measured on ahra: `cohere --lint modules/tasks/data/attachments/pt64fq7/score.mjs`
// printed exactly that, with nothing saying the file is left out by the tsconfig's `exclude` and
// ignored by git besides, so a reader could not tell a scratch file from a broken invocation. The
// refusal stays; it now carries the reason.
//
// Git is asked rather than the ignore files parsed, because git is what decides: `check-ignore -v`
// names the file, the line and the pattern that matched, which is what a reader needs to change it.
// A path git cannot answer about, outside any repository, simply gets no git clause.
func explainNamedPathsOutsideProgram(location projectLocation, names []string) string {
	sentences := make([]string, 0, len(names))
	configName := location.ConfigFileName
	if relative, err := filepath.Rel(location.Root, configName); err == nil && !strings.HasPrefix(relative, "..") {
		configName = relative
	}

	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(location.ArgumentBase, name)
		}
		path = filepath.Clean(path)

		var sentence string
		information, err := os.Stat(path)
		switch {
		case err == nil && information.IsDir():
			sentence = name + " is a directory holding no file the program contains, because the tsconfig at " +
				configName + " leaves every file in it out with its `include` and `exclude` lists"
		case !programExtensions[strings.ToLower(filepath.Ext(path))]:
			sentence = name + " is not a TypeScript or JavaScript file, so no tsconfig can put it in the program"
		default:
			sentence = name + " is not one of the program's files, because the tsconfig at " + configName +
				" leaves it out with its `include` and `exclude` lists"
		}

		if ignoredBy := gitIgnoreSource(location.Root, path); ignoredBy != "" {
			sentence += ", and git ignores it too (" + ignoredBy + ")"
		}
		sentences = append(sentences, sentence+".")
	}

	return "nothing to check: none of the named paths is in the program. " + strings.Join(sentences, " ")
}

// gitIgnoreSource names the ignore rule that matches a path, as the ignore file and line followed by
// the pattern in backticks, or answers empty when git does not ignore it or cannot say.
//
// `git check-ignore` exits 1 for a path it does not ignore and 128 outside a repository; both are
// "no clause" rather than a failure, because the explanation is complete without one.
func gitIgnoreSource(root string, path string) string {
	command := exec.Command("git", "check-ignore", "--verbose", "--", path)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		// Not ignored, outside a repository, or no git at all: the sentence stands without it.
		return ""
	}

	// The verbose form is `<source>:<line>:<pattern>\t<path>`. The source and line never contain a
	// tab, and a pattern cannot either, so the tab is the one safe split.
	line := strings.TrimSpace(string(output))
	match, _, found := strings.Cut(line, "\t")
	if !found {
		return ""
	}
	source, rest, found := strings.Cut(match, ":")
	if !found {
		return ""
	}
	lineNumber, pattern, found := strings.Cut(rest, ":")
	if !found {
		return ""
	}
	return source + ":" + lineNumber + " `" + pattern + "`"
}
