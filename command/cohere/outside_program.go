package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/format/formatfiles"
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
// ignored by .gitignore besides, so a reader could not tell a scratch file from a broken invocation.
// The refusal stays; it now carries the reason, with the ignore line that matched when one does,
// which is what a reader needs to change it. See ignoreSource.
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

		if ignoredBy := ignoreSource(location.Root, path); ignoredBy != "" {
			sentence += ", and the root's .gitignore leaves it out too (" + ignoredBy + ")"
		}
		sentences = append(sentences, sentence+".")
	}

	return "nothing to check: none of the named paths is in the program. " + strings.Join(sentences, " ")
}

// ignoreSource names the line of the project root's `.gitignore` that covers a path, with the pattern
// in backticks, or answers empty when none does or the file cannot be read.
//
// Read natively, from the root's `.gitignore`, with the matcher the format walk uses. Git used to be
// asked (`check-ignore --verbose`), which also saw nested ignore files and global excludes; this clause
// is part of an explanation and decides nothing, so the narrower answer costs a reader at most a
// missing clause, and cohere runs no git.
func ignoreSource(root string, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	line, pattern, err := formatfiles.IgnoringLine(filepath.Join(root, ".gitignore"), relative)
	if err != nil || line == 0 {
		return ""
	}
	return fmt.Sprintf("line %d, `%s`", line, pattern)
}
