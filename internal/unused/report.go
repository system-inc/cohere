package unused

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/program"
)

// Run produces the whole unused report: what nothing references, and what nothing can reach.
//
// The two halves are computed together and printed together because they read as one question to a
// person — "what did I write that is not doing anything" — while being two analyses with different
// evidence behind them. The report keeps them in separate sections precisely so the reader can trust
// them differently, which is the honest presentation of two claims of unequal strength.
func Run(ctx context.Context, graph *program.Graph, files []*ast.SourceFile) (*Report, error) {
	roots := buildRootSet(files)

	report, err := FindUnreferenced(ctx, graph, files, roots)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		report.Unreachable = append(report.Unreachable, FindUnreachable(file)...)
	}

	return report, nil
}

// buildRootSet collects the conventions that make a file alive regardless of what imports it.
//
// The dynamic-import directories are discovered from the program rather than configured, so a
// directory that becomes dynamically loaded is covered the day it is written.
func buildRootSet(files []*ast.SourceFile) *RootSet {
	roots := &RootSet{}

	seen := map[string]bool{}
	for _, file := range files {
		for _, directory := range computedImportDirectories(file) {
			if !seen[directory] {
				seen[directory] = true
				roots.dynamicImportDirectories = append(roots.dynamicImportDirectories, directory)
			}
		}
	}
	return roots
}

// computedImportDirectories finds the directories a template-literal `import()` can reach.
//
// # Why this exists, measured rather than imagined
//
// This is the class that would have done the most damage to the report's credibility. On the ahra
// tree, 532 locale files live under `translations/` directories and are loaded exclusively as
// `import(`@structure/source/.../translations/${localeCode}`)`. Not one of them has a static import
// anywhere. A reference index without this treats every one as unused, and a report whose first
// screen is 532 live translation files is a report nobody reads to the second screen.
//
// The specifier is computed at runtime, so no static analysis can resolve which file a given
// `${localeCode}` selects. The honest move is therefore to root the whole directory the literal
// prefix points into, rather than to pretend a reference was found or to resolve a specifier that
// genuinely is not knowable until the program runs.
func computedImportDirectories(file *ast.SourceFile) []string {
	var directories []string
	fileName := file.FileName()

	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if node == nil {
			return false
		}
		if node.Kind == ast.KindCallExpression {
			call := node.AsCallExpression()
			if call.Expression != nil && call.Expression.Kind == ast.KindImportKeyword &&
				call.Arguments != nil && len(call.Arguments.Nodes) > 0 {
				argument := call.Arguments.Nodes[0]
				if argument.Kind == ast.KindTemplateExpression {
					if prefix := templateHeadText(argument); prefix != "" {
						if directory := resolveSpecifierDirectory(prefix, fileName); directory != "" {
							directories = append(directories, directory)
						}
					}
				}
			}
		}
		node.ForEachChild(visit)
		return false
	}
	file.AsNode().ForEachChild(visit)
	return directories
}

// templateHeadText returns the literal text before the first substitution in a template.
func templateHeadText(node *ast.Node) string {
	template := node.AsTemplateExpression()
	if template == nil || template.Head == nil {
		return ""
	}
	return template.Head.Text()
}

// resolveSpecifierDirectory turns the literal prefix of a computed specifier into a directory
// suffix that a file path can be matched against.
//
// It deliberately returns a path SUFFIX rather than a resolved absolute path. Resolving the alias
// (`@structure/...`) would mean reimplementing module resolution for a specifier that is not fully
// known, and getting that wrong fails silently in the direction that reports live files as dead.
// Matching on the trailing directory is coarser and cannot make that mistake.
func resolveSpecifierDirectory(prefix string, fileName string) string {
	trimmed := strings.TrimSuffix(prefix, "/")
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndex(trimmed, "/")
	if index < 0 {
		return ""
	}
	// The last literal segment is the directory the computed part selects a file from.
	segment := trimmed[index+1:]
	if segment == "" || strings.Contains(segment, "$") {
		return ""
	}
	_ = fileName
	return "/" + segment + "/"
}

// Write prints the report.
//
// The summary line comes first and states the population as well as the findings, because a count
// with no denominator cannot be told apart from a broken probe. A report saying "0 unused" over a
// tree of 3,400 files is far more likely to be a broken analysis than a clean codebase, and the
// only thing that separates those two readings is the denominator printed beside the zero.
func Write(out io.Writer, report *Report, showIntentional bool) {
	fmt.Fprintf(out, "\nunused: a report, not a gate — nothing here fails a build\n")

	intentionalFiles := 0
	for _, file := range report.Files {
		if file.Intentional.Present {
			intentionalFiles++
		}
	}
	intentionalExports := 0
	for _, finding := range report.Unreferenced {
		if finding.Intentional.Present {
			intentionalExports++
		}
	}

	writeFileSection(out, report, showIntentional, intentionalFiles)
	writeExportSection(out, report, showIntentional, intentionalExports)
	writeUnreachableSection(out, report)

	// The coverage line, in the same spirit as the lint phase's. It states what was looked at, not
	// only what was found, so a run that analyzed nothing cannot read like a run that found nothing.
	fmt.Fprintf(out,
		"  looked at %d files (%d spared by a framework or dynamic-load convention) and %d exported declarations\n",
		report.FilesAnalyzed, report.RootedFiles, report.ExportsAnalyzed,
	)
}

func writeFileSection(out io.Writer, report *Report, showIntentional bool, intentional int) {
	if len(report.Files) == 0 {
		return
	}
	sort.Slice(report.Files, func(left, right int) bool {
		return report.Files[left].FileName < report.Files[right].FileName
	})

	fmt.Fprintf(out, "\n  files whose every export is unreferenced — %s\n", countPhrase(len(report.Files), intentional))
	for _, file := range report.Files {
		if file.Intentional.Present && !showIntentional {
			continue
		}
		marker := ""
		if file.Intentional.Present {
			marker = intentionalSuffix(file.Intentional)
		}
		fmt.Fprintf(out, "    %s — %d exports, none used%s\n", file.FileName, file.Exports, marker)
	}
}

func writeExportSection(out io.Writer, report *Report, showIntentional bool, intentional int) {
	if len(report.Unreferenced) == 0 {
		return
	}
	sort.Slice(report.Unreferenced, func(left, right int) bool {
		if report.Unreferenced[left].FileName != report.Unreferenced[right].FileName {
			return report.Unreferenced[left].FileName < report.Unreferenced[right].FileName
		}
		return report.Unreferenced[left].Name < report.Unreferenced[right].Name
	})

	fmt.Fprintf(out, "\n  exports nothing references — %s\n", countPhrase(len(report.Unreferenced), intentional))
	for _, finding := range report.Unreferenced {
		if finding.Intentional.Present && !showIntentional {
			continue
		}
		fmt.Fprintf(out, "    %s — %s %s%s\n",
			finding.FileName, finding.Kind, finding.Name, intentionalSuffix(finding.Intentional))
	}
}

func writeUnreachableSection(out io.Writer, report *Report) {
	if len(report.Unreachable) == 0 {
		return
	}
	sort.Slice(report.Unreachable, func(left, right int) bool {
		if report.Unreachable[left].FileName != report.Unreachable[right].FileName {
			return report.Unreachable[left].FileName < report.Unreachable[right].FileName
		}
		return report.Unreachable[left].Range.Position < report.Unreachable[right].Range.Position
	})

	fmt.Fprintf(out, "\n  statements nothing can reach — %d\n", len(report.Unreachable))
	for _, finding := range report.Unreachable {
		fmt.Fprintf(out, "    %s:%d — %s\n", finding.FileName, finding.Range.Position, finding.Cause)
	}
}

// countPhrase reads `18 unused, 4 declared intentional` rather than 22 undifferentiated lines, so
// the reader can see at a glance how much of the report is already judged.
func countPhrase(total int, intentional int) string {
	if intentional == 0 {
		return fmt.Sprintf("%d", total)
	}
	return fmt.Sprintf("%d, %d declared intentional with %s", total-intentional, intentional, intentionalMarker)
}

func intentionalSuffix(reason intentionalReason) string {
	if !reason.Present {
		return ""
	}
	if reason.Text == "" {
		return fmt.Sprintf("  [%s]", intentionalMarker)
	}
	return fmt.Sprintf("  [%s: %s]", intentionalMarker, reason.Text)
}
