// Command linewrap rewraps Go lines wider than 120 columns without changing what the program means.
//
// Usage: linewrap [-w] [-logs directory] [-skip path]... [-exempt-keyed-strings] root
//
// It walks every Go file under root, skipping TypeScript/, testdata/, generated files and every -skip path (a
// file, or a directory and everything under it, relative to root), and prints counts of
// long lines before and after, the residue, and any file whose output failed the equivalence proof. With -w it
// writes the files that changed and passed the proof.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
)

// skipList collects every -skip flag.
type skipList []string

func (list *skipList) String() string {
	return strings.Join(*list, ",")
}

func (list *skipList) Set(value string) error {
	*list = append(*list, strings.TrimSuffix(filepath.ToSlash(filepath.Clean(value)), "/"))
	return nil
}

// covers reports whether a slash-separated path relative to root is a skipped path or lies under one.
func (list skipList) covers(relative string) bool {
	for _, skipped := range list {
		if relative == skipped || strings.HasPrefix(relative, skipped+"/") {
			return true
		}
	}
	return false
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code linewrap exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs linewrap, returning the code it exits with.
func runCommandLine() int {
	write := flag.Bool("w", false, "write changed files in place")
	logs := flag.String("logs", "", "directory for linewrap-before.txt, linewrap-after.txt and linewrap-residue.txt")
	var skips skipList
	flag.Var(&skips, "skip", "a path relative to root to leave untouched, a file or a directory; repeatable")
	var options Options
	flag.BoolVar(&options.ExemptKeyedStrings, "exempt-keyed-strings", false,
		"exempt a line starting with `key: `, `name = ` or `name := ` and a string literal followed only by closing punctuation")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(
			streams.Errors,
			"usage: linewrap [-w] [-logs directory] [-skip path]... [-exempt-keyed-strings] root",
		)
		return 2
	}
	if runError := run(flag.Arg(0), *write, *logs, skips, options); runError != nil {
		fmt.Fprintln(streams.Errors, runError)
		return 1
	}
	return 0
}

func run(root string, write bool, logs string, skips skipList, options Options) error {
	before := Count{}
	after := Count{}
	generated := 0
	skippedFiles := 0
	skippedLines := 0
	residueByShape := map[string]int{}
	var residue []string
	var rejected []string
	var failed []string
	changedFiles := 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == "TypeScript" ||
				entry.Name() == "testdata" ||
				(strings.HasPrefix(entry.Name(), ".") && relative != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if skips.covers(relative) {
			skippedFiles++
			if parsed, parseSourceError := parseSource(source, Options{}); parseSourceError == nil {
				skippedLines += len(parsed.longLines())
			}
			return nil
		}
		header, parseFileError := parser.ParseFile(
			token.NewFileSet(),
			"",
			source,
			parser.ParseComments|parser.PackageClauseOnly,
		)
		// A copy of upstream source (a `cohere:copied` header before the package clause) is foreign code kept
		// byte for byte, so it is left exactly as a generated file is.
		if parseFileError == nil && (ast.IsGenerated(header) || declaresCopy(header)) {
			if parsed, parseSourceError := parseSource(source, Options{}); parseSourceError == nil {
				generated += len(parsed.longLines())
			}
			return nil
		}
		result, wrapError := Wrap(source, options)
		if wrapError != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", relative, wrapError))
			return nil
		}
		for category, count := range result.Before {
			before[category] += count
		}
		if result.Rejected != nil {
			rejected = append(rejected, fmt.Sprintf("%s: %v", relative, result.Rejected))
			for category, count := range result.Before {
				after[category] += count
			}
			return nil
		}
		for category, count := range result.After {
			after[category] += count
		}
		for _, line := range result.Residue {
			residueByShape[line.Shape]++
			residue = append(residue, fmt.Sprintf("%s:%d: %s: %s", relative, line.Line, line.Shape, line.Text))
		}
		if string(result.Output) != string(source) {
			changedFiles++
			if write {
				return os.WriteFile(path, result.Output, entry.Type().Perm()|0o644)
			}
		}
		return nil
	})
	if walkError != nil {
		return walkError
	}
	summary := func(title string, count Count) string {
		var builder strings.Builder
		total := 0
		for _, value := range count {
			total += value
		}
		fmt.Fprintf(
			&builder,
			"%s: %d long lines outside generated and skipped files (untouched: generated files hold %d more, %d skipped files hold %d more)\n",
			title,
			total,
			generated,
			skippedFiles,
			skippedLines,
		)
		for _, category := range Categories {
			fmt.Fprintf(&builder, "  %-14s %d\n", category, count[category])
		}
		return builder.String()
	}
	var shapes []string
	for shape := range residueByShape {
		shapes = append(shapes, shape)
	}
	sort.Strings(shapes)
	var residueReport strings.Builder
	fmt.Fprintf(&residueReport, "residue: %d actionable lines still long\n", len(residue))
	for _, shape := range shapes {
		fmt.Fprintf(&residueReport, "  %-20s %d\n", shape, residueByShape[shape])
	}
	fmt.Fprintf(&residueReport, "rejected by the equivalence proof: %d\n", len(rejected))
	for _, line := range rejected {
		fmt.Fprintln(&residueReport, "  "+line)
	}
	fmt.Fprintf(&residueReport, "failed to process: %d\n", len(failed))
	for _, line := range failed {
		fmt.Fprintln(&residueReport, "  "+line)
	}
	fmt.Fprintln(&residueReport)
	for _, line := range residue {
		fmt.Fprintln(&residueReport, line)
	}
	beforeText := summary("before", before)
	afterText := summary("after", after) + fmt.Sprintf("files changed: %d\n", changedFiles)
	fmt.Fprint(streams.Output, beforeText, afterText, strings.SplitN(residueReport.String(), "\n\n", 2)[0], "\n")
	if logs == "" {
		return nil
	}
	for name, text := range map[string]string{
		"linewrap-before.txt":  beforeText,
		"linewrap-after.txt":   afterText,
		"linewrap-residue.txt": residueReport.String(),
	} {
		if writeError := os.WriteFile(filepath.Join(logs, name), []byte(text), 0o644); writeError != nil {
			return writeError
		}
	}
	return nil
}
