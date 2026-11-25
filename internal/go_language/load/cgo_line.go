package load

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
)

// lineDirectivePrefix starts a `//line` directive, which go/scanner reads only at the start of a line.
const lineDirectivePrefix = "//line "

// cgoOutputsOf is each cgo output the load parses, by its absolute path, with the package whose translation it is:
// every compiled file of a translated package outside the package's directory, which go list names in the build
// cache. A package and its test variant hold the same outputs and the same listed package.
func cgoOutputsOf(built *graph) map[string]*listedPackage {
	outputs := map[string]*listedPackage{}
	for _, current := range built.nodes {
		if !current.translated {
			continue
		}
		for _, file := range current.loaded.CompiledGoFiles {
			if !inside(current.listed.Directory, file) {
				outputs[file] = current.listed
			}
		}
	}
	return outputs
}

// rewriteCgoLines is a cgo output's contents with each `//line` directive that names one of listedPackage's cgo files
// in another directory naming it in the package's directory, and true, or contents itself and false when none does.
// go keys the translation of a package under `GOROOT` on its files' contents and not on its directory, so two `GOROOT`
// copies of one version at different paths share one translation in a shared `GOCACHE`, and its directives name the
// copy that made it, gone once that copy is (#gpmr5z0). Every position in the package, and cgocall's reading of the
// raw file, follows the directives, so each would name a file that no longer exists. The rewrite is exact: the key
// holds the contents, so the file the directive names and the one of that name in the package's directory are the
// same bytes.
//
// A directive is rewritten only when the file it names is one of the package's CgoFiles, every directive naming a
// cgo file of the package names that one directory and it is not the package's own, and the file exists in the
// package's directory; any other directive is left as it is, as is every directive when two directories are named,
// since which is the package's would be a guess. Only the directory changes, so every line and column the directive
// gives is unchanged. contents may be held bytes shared with other readers, so it is never written: a rewrite is a
// copy, which the load gives every reader of the file's source (see Rewritten). cgo writes `//line` directives with a
// file and `/*line :line:column*/` ones without, which keep the file the last `//line` named, so the line form is the
// only one to read.
func rewriteCgoLines(contents []byte, listedPackage *listedPackage) ([]byte, bool) {
	named := map[string]bool{}
	for line := range bytes.Lines(contents) {
		if file, isDirective := cgoFileNamed(line, listedPackage); isDirective {
			named[filepath.Dir(file)] = true
		}
	}
	if len(named) != 1 || named[listedPackage.Directory] {
		return contents, false
	}
	var rewritten bytes.Buffer
	rewritten.Grow(len(contents))
	changed := false
	for line := range bytes.Lines(contents) {
		file, isDirective := cgoFileNamed(line, listedPackage)
		if !isDirective {
			rewritten.Write(line)
			continue
		}
		current := filepath.Join(listedPackage.Directory, filepath.Base(file))
		if !exists(current) {
			rewritten.Write(line)
			continue
		}
		// The directive is the prefix, then the file, then the line and column, which are kept byte for byte.
		rewritten.WriteString(lineDirectivePrefix + current)
		rewritten.Write(line[len(lineDirectivePrefix)+len(file):])
		changed = true
	}
	if !changed {
		return contents, false
	}
	return rewritten.Bytes(), true
}

// cgoFileNamed is the file a `//line` directive names, as written, when line is one and the file is absolute and has
// the name of one of listedPackage's CgoFiles, and false otherwise.
func cgoFileNamed(line []byte, listedPackage *listedPackage) (string, bool) {
	// Every line of every cgo output reaches here on every load, so a line that is no directive is turned away before
	// it is copied into a string.
	if !bytes.HasPrefix(line, []byte(lineDirectivePrefix)) {
		return "", false
	}
	text := string(line[len(lineDirectivePrefix):])
	file, hasPosition := directiveFile(strings.TrimRight(text, "\r\n"))
	if !hasPosition || !filepath.IsAbs(file) || !slices.Contains(listedPackage.CgoFiles, filepath.Base(file)) {
		return "", false
	}
	return file, true
}

// directiveFile is the file of a `//line` directive's text after the prefix, as go/scanner splits it: the text before
// the last colon followed by a line number, or before the one ahead of that when it too is followed by a number, the
// line then the column. It is false when the text ends in no number.
func directiveFile(text string) (string, bool) {
	colon := strings.LastIndexByte(text, ':')
	if colon < 0 || !isNumber(text[colon+1:]) {
		return "", false
	}
	if earlier := strings.LastIndexByte(text[:colon], ':'); earlier >= 0 && isNumber(text[earlier+1:colon]) {
		return text[:earlier], true
	}
	return text[:colon], true
}

// isNumber reports whether text is one or more decimal digits.
func isNumber(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}
