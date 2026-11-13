// Package copied reads the header that declares a Go file a copy of upstream source: foreign code we carry, byte
// for byte, until upstream ships what we needed from it. The engine withholds house rules' findings in such a file
// and counts them, as it does for a generated file, while vet, tools and staticcheck still judge it. Restyling a
// copy to the house would bury the one thing that matters about it, its diff against upstream.
//
// A copy declares itself in the comments before its package clause, in two lines, the second right below the
// first:
//
//	// cohere:copied <module>@<version> <path>
//	// cohere:until <what upstream has to do before the copy can go>
//
// <module> is a module path and <version> its canonical semantic version, as the module cache spells them, and
// <path> is the slash-separated path, from the module's root, of the one upstream file this file copies. A copy of the
// Go toolchain's own source names the module std, as go list names the standard library, and a Go release as go
// spells it, std@go1.27.0, with <path> below the toolchain's src directory, as go/build/read.go. The until
// text runs on through the lines of its comment below it, to the first blank comment line or the comment's end, and
// is never empty: a copy says when it goes away.
//
// Every line of the file outside its marked changes is upstream's, which a test beside this file holds true
// against the module cache, or a std copy against the toolchain's source, which the copy's own package also holds to
// the sources of the release the copy names, so a toolchain that moves past them fails until the copy is bumped. A
// marked change is a run of lines that differ from upstream's and hold a comment with `cohere:` in it, the header
// among them; lines dropped from upstream are marked by such a comment where they were.
// A file we translated rather than copied is ours and declares nothing here.
//
// A header that breaks this grammar is an error, never a file read as ours by accident: IsCopied is false for it,
// so the house rules report on the file again, and the test fails naming it.
package copied

import (
	"fmt"
	"go/ast"
	"go/version"
	"path"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// The two directives, spelled as a header writes them.
const (
	copiedDirective = "cohere:copied"
	untilDirective  = "cohere:until"
)

// StandardLibrary is the module a copy of the Go toolchain's own source names, as go list names the standard library.
const StandardLibrary = "std"

// Header is what a copy's header declares.
type Header struct {
	// Module and Version name the upstream module, as golang.org/x/tools and v0.50.0, or the toolchain, as std and
	// go1.27.0.
	Module  string
	Version string

	// Path is the upstream file's path from the module's root, slash-separated, as go/analysis/checker/checker.go.
	Path string

	// Until is what upstream has to do before the copy can go, its lines joined by spaces.
	Until string
}

// IsCopied reports whether file declares itself a copy with a header the grammar allows, reading only the comments
// before its package clause, as go/ast.IsGenerated does.
func IsCopied(file *ast.File) bool {
	_, declared, readError := Read(file)
	return declared && readError == nil
}

// Read is file's header. declared is false, with no error, when file declares no copy; it is true whenever a
// comment before the package clause opens with either directive, and then an error says how the header breaks the
// grammar.
func Read(file *ast.File) (header Header, declared bool, readError error) {
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for index, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			switch {
			case strings.HasPrefix(text, copiedDirective):
				if declared {
					return Header{}, true, fmt.Errorf("reading the copy header: %q is a second %s line, and a "+
						"copy declares one upstream file", comment.Text, copiedDirective)
				}
				declared = true
				parsed, parseError := parseCopied(comment.Text)
				if parseError != nil {
					return Header{}, true, parseError
				}
				until, untilError := readUntil(group.List[index+1:])
				if untilError != nil {
					return Header{}, true, untilError
				}
				parsed.Until = until
				header = parsed
			case strings.HasPrefix(text, untilDirective) && !followsCopied(group.List[:index]):
				return Header{}, true, fmt.Errorf("reading the copy header: %q has no %s line right above it",
					comment.Text, copiedDirective)
			}
		}
	}
	return header, declared, nil
}

// parseCopied reads the copied line, which is exactly `// cohere:copied <module>@<version> <path>`.
func parseCopied(line string) (Header, error) {
	arguments, spelled := strings.CutPrefix(line, "// "+copiedDirective+" ")
	if !spelled {
		return Header{}, fmt.Errorf("reading the copy header: %q is not spelled // %s <module>@<version> <path>",
			line, copiedDirective)
	}
	fields := strings.Fields(arguments)
	if len(fields) != 2 {
		return Header{}, fmt.Errorf("reading the copy header: %q has %d fields after %s, want <module>@<version> "+
			"and <path>", line, len(fields), copiedDirective)
	}
	modulePath, release, versioned := strings.Cut(fields[0], "@")
	if !versioned || release == "" {
		return Header{}, fmt.Errorf("reading the copy header: %q names no version of %s", line, modulePath)
	}
	if modulePath == StandardLibrary {
		// A release names a patch, as go1.27.0, where go1.27 is a language version that no one toolchain is.
		if !version.IsValid(release) || version.Lang(release) == release {
			return Header{}, fmt.Errorf("reading the copy header: %q names %s, which is no Go release, as go1.27.0",
				line, release)
		}
	} else {
		if checkError := module.Check(modulePath, release); checkError != nil {
			return Header{}, fmt.Errorf("reading the copy header %q: %w", line, checkError)
		}
		if semver.Canonical(release) != release {
			return Header{}, fmt.Errorf("reading the copy header: %q writes version %s, which the module cache "+
				"spells %s", line, release, semver.Canonical(release))
		}
	}
	upstreamPath := fields[1]
	if path.IsAbs(upstreamPath) || path.Clean(upstreamPath) != upstreamPath || upstreamPath == ".." ||
		strings.HasPrefix(upstreamPath, "../") || path.Ext(upstreamPath) != ".go" {
		return Header{}, fmt.Errorf("reading the copy header: %q names %s, which is no clean path of a Go file "+
			"inside the module", line, upstreamPath)
	}
	return Header{Module: modulePath, Version: release, Path: upstreamPath}, nil
}

// readUntil reads the until line, which must be the first of below, and the lines it runs on through.
func readUntil(below []*ast.Comment) (string, error) {
	if len(below) == 0 {
		return "", fmt.Errorf("reading the copy header: no %s line right below the %s line, so the copy never "+
			"says when it goes away", untilDirective, copiedDirective)
	}
	reason, spelled := strings.CutPrefix(below[0].Text, "// "+untilDirective)
	if !spelled {
		return "", fmt.Errorf("reading the copy header: %q is where the %s line goes, right below the %s line",
			below[0].Text, untilDirective, copiedDirective)
	}
	if reason != "" && !strings.HasPrefix(reason, " ") {
		return "", fmt.Errorf("reading the copy header: %q is not spelled // %s <reason>", below[0].Text,
			untilDirective)
	}
	words := []string{strings.TrimSpace(reason)}
	for _, continued := range below[1:] {
		line, isLine := strings.CutPrefix(continued.Text, "//")
		if !isLine || strings.TrimSpace(line) == "" {
			break
		}
		words = append(words, strings.TrimSpace(line))
	}
	until := strings.TrimSpace(strings.Join(words, " "))
	if until == "" {
		return "", fmt.Errorf("reading the copy header: the %s line says nothing, so the copy never says when it "+
			"goes away", untilDirective)
	}
	return until, nil
}

// followsCopied reports whether the last of above is a copied line, which an until line must follow.
func followsCopied(above []*ast.Comment) bool {
	if len(above) == 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(above[len(above)-1].Text, "//")), copiedDirective)
}
