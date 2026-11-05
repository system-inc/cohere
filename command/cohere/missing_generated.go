package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// A generated file the program asks for and nobody has generated yet reads as one cause (#wxsqavy).
//
// www-phi-health's first gated CI run read 61 type errors, every one of them because next-env.d.ts imports
// .next/types/routes.d.ts, where Next declares PageProps, LayoutProps and RouteContext, and a fresh checkout
// has no .next until `next typegen` writes it. Sixty-one errors read as sixty-one problems.
//
// So the missing file leads, one line each with its remedy, and in the human view the type errors fold under
// it as a count by code. They are folded, never dropped: a real error can hide among them, so they still
// fail the run, `--verbose` and `--json` print every one, and the footer names the missing file as a gap,
// which also fails the run when no type error follows from it. A green run never comes from a file that
// was not there to check.
//
// Every lint fix is held while one is missing (#46ern7e). A rule reads the names the missing file declares
// as unresolved, whether or not it asks the checker: on a fresh Next clone,
// structure/react-component-require-properties-type-suffix took PageProps for the project's own type and
// renamed it to PageProperties in 39 files, and a bare run wrote them. The fix phase then runs as
// `--format-only` does, since formatting reads no names, and the footer says the fixes were held.

// missingGeneratedRule is the name the cause prints under, where a finding prints its rule.
const missingGeneratedRule = "missing-generated-file"

// generatedOutputUnder answers whether an absolute path is generated output: inside root and ignored by
// git there. Outside root, or when root's ignore files cannot be read, nothing is, so the cause is never
// claimed on a guess.
func generatedOutputUnder(root string) func(target string) bool {
	matcher, newGitignoreError := gitignore.New(root)
	return func(target string) bool {
		if newGitignoreError != nil {
			return false
		}
		relative, relativeError := filepath.Rel(root, filepath.FromSlash(target))
		if relativeError != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false
		}
		ignored, _, ignoreError := matcher.IgnoredPath(filepath.ToSlash(relative), false)
		return ignoreError == nil && ignored
	}
}

// missingGeneratedRemedy is what writes the file, when that is known from where it lives.
func missingGeneratedRemedy(target string) string {
	if strings.Contains(filepath.ToSlash(target), "/.next/types/") {
		return "`next typegen` writes it, so run that before cohere"
	}
	return "the build step that writes it has not run, so run that before cohere"
}

// missingGeneratedMessage is the cause as its line says it.
func missingGeneratedMessage(input program.MissingInput) string {
	return fmt.Sprintf(
		"%s does not exist: it is generated output (git ignores it), and %s",
		input.Specifier,
		missingGeneratedRemedy(input.Target),
	)
}

// printMissingGenerated prints each missing generated file as a finding, in every view, ahead of anything
// the run found.
func printMissingGenerated(missing []program.MissingInput) {
	for _, input := range missing {
		message := missingGeneratedMessage(input)
		printFinding(streams.Output, runFinding{
			Path:     input.Importer,
			Line:     input.Line,
			Column:   input.Column,
			Severity: "error",
			Rule:     missingGeneratedRule,
			Message:  message,
		}, fmt.Sprintf(
			"%s:%d:%d - error %s: %s\n",
			input.Importer,
			input.Line,
			input.Column,
			missingGeneratedRule,
			message,
		))
	}
}

// foldTypeDiagnostics prints, in the human view, one line in place of the type errors when a generated file
// is missing, and reports whether it did. Every other view prints them all.
func foldTypeDiagnostics(missing []program.MissingInput, diagnostics []*ast.Diagnostic) bool {
	if len(missing) == 0 || len(diagnostics) == 0 || activeOutput.Mode != outputHuman {
		return false
	}
	fmt.Fprintln(streams.Output, activeOutput.Style.dim(foldedLine(diagnostics)))
	return true
}

// foldedLine is the type errors as their count by code, most frequent first, then by code.
func foldedLine(diagnostics []*ast.Diagnostic) string {
	byCode := map[int32]int{}
	for _, diagnostic := range diagnostics {
		byCode[diagnostic.Code()]++
	}
	codes := make([]int32, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(left, right int) bool {
		if byCode[codes[left]] != byCode[codes[right]] {
			return byCode[codes[left]] > byCode[codes[right]]
		}
		return codes[left] < codes[right]
	})
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%d TS%d", byCode[code], code))
	}
	return fmt.Sprintf("  %s folded under it, most likely its cascade (%s): --verbose prints every one",
		counted(len(diagnostics), "type error", "type errors"), strings.Join(parts, ", "))
}

// missingGeneratedTargets is each missing file once, relative to root, for the footer and `--json`.
func missingGeneratedTargets(missing []program.MissingInput, root string) []string {
	seen := map[string]bool{}
	var targets []string
	for _, input := range missing {
		target := input.Target
		if relative, relativePathError := filepath.Rel(root, filepath.FromSlash(target)); relativePathError == nil {
			target = filepath.ToSlash(relative)
		}
		if !seen[target] {
			seen[target] = true
			targets = append(targets, target)
		}
	}
	return targets
}
