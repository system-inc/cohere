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
//
// The hold covers every input the program could not load, not only a generated one (#4jxsbzr). Loom's wire
// project, with no node_modules, had its tsconfig's types entries and its package imports resolve to
// nothing, and a bare run still let nexus/consistency-require-identifier-casing rewrite two files. An
// import, a reference or a types entry that resolves to nothing prints its own line, once per name at its
// first site, and holds the fixes the same way. It is a warning rather than an error: wherever the compiler
// checks the file, it reports the same input as a type error, which is what fails the run, and this line
// says what that did to the fix phase. The fold stays a generated file's: without one, the type errors are
// what name each missing input, so they print as they are.

// missingGeneratedRule is the name the cause prints under, where a finding prints its rule.
const missingGeneratedRule = "missing-generated-file"

// unresolvedInputRule is the name an input the program could not load, and did not generate, prints under.
const unresolvedInputRule = "unresolved-input"

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

// unresolvedInputMessage is an input the program could not load as its line says it, with how many other
// sites ask for the same one, since only the first prints.
func unresolvedInputMessage(input program.MissingInput, otherSites int) string {
	var message string
	switch input.Kind {
	case program.UnresolvedTypes:
		message = fmt.Sprintf(
			"%s resolves to no type library: no file or installed package answers it, so every lint fix is held "+
				"until one does",
			input.Specifier,
		)
	case program.UnresolvedReference:
		message = fmt.Sprintf("%s does not exist, so every lint fix is held until it does", input.Specifier)
	default:
		message = fmt.Sprintf(
			"%s resolves to nothing: no file, installed package or ambient declaration answers it, so every lint "+
				"fix is held until one does",
			input.Specifier,
		)
	}
	if otherSites > 0 {
		message += fmt.Sprintf(" (and %s)", counted(otherSites, "other site", "other sites"))
	}
	return message
}

// printMissingInputs prints each input the program could not load as a finding, in every view, ahead of
// anything the run found: every missing generated file, and each other input once, at its first site.
func printMissingInputs(missing []program.MissingInput) {
	sites := map[string]int{}
	for _, input := range missing {
		if input.Kind != program.MissingGenerated {
			sites[input.Target]++
		}
	}
	printed := map[string]bool{}
	for _, input := range missing {
		severity, rule := "error", missingGeneratedRule
		var message string
		if input.Kind == program.MissingGenerated {
			message = missingGeneratedMessage(input)
		} else {
			if printed[input.Target] {
				continue
			}
			printed[input.Target] = true
			severity, rule = "warning", unresolvedInputRule
			message = unresolvedInputMessage(input, sites[input.Target]-1)
		}
		printFinding(streams.Output, runFinding{
			Path:     input.Importer,
			Line:     input.Line,
			Column:   input.Column,
			Severity: severity,
			Rule:     rule,
			Message:  message,
		}, fmt.Sprintf(
			"%s:%d:%d - %s %s: %s\n",
			input.Importer,
			input.Line,
			input.Column,
			severity,
			rule,
			message,
		))
	}
}

// generatedMissing reports whether any missing input is a generated file, which is what the fold, the gap
// and the fix phase's note are about.
func generatedMissing(missing []program.MissingInput) bool {
	for _, input := range missing {
		if input.Kind == program.MissingGenerated {
			return true
		}
	}
	return false
}

// foldTypeDiagnostics prints, in the human view, one line in place of the type errors when a generated file
// is missing, and reports whether it did. Every other view prints them all.
func foldTypeDiagnostics(missing []program.MissingInput, diagnostics []*ast.Diagnostic) bool {
	if !generatedMissing(missing) || len(diagnostics) == 0 || activeOutput.Mode != outputHuman {
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

// missingGeneratedTargets is each missing generated file once, relative to root, for the footer and `--json`.
func missingGeneratedTargets(missing []program.MissingInput, root string) []string {
	seen := map[string]bool{}
	var targets []string
	for _, input := range missing {
		if input.Kind != program.MissingGenerated {
			continue
		}
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
