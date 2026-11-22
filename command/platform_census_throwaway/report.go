package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// censusNotes say what the census counted and how, every place a count is a choice rather than go/types' own answer.
var censusNotes = []string{
	"Each file is counted once per build, under the first package by id that compiles it, so a test variant's " +
		"recompile of a package is not counted again; each declaration once by package path and name.",
	"A declaration's answer is its kind, its type with packages spelled by path, a constant's exact value, and for a " +
		"named type its underlying type too, since a named type's own type string is its name alone.",
	"An expression's answer is its recorded type with packages spelled by path and its exact constant value; " +
		"expressions are those go/types records in Info.Types, so an identifier being declared is not one, and a " +
		"local's changed type shows at its uses.",
	"cgo's output is compiled from the build cache under a path of its own, so a package one side compiles through " +
		"cgo pairs none of those files with the other side; they are counted as files one side only (outside the " +
		"package's directory) and their expressions are not aligned.",
	"A fork is by reference when its subtree, on either side, holds an identifier or selector whose used object is a " +
		"differing declaration, a member of a differing named type included; otherwise local. A fork reached " +
		"through a local variable is local: the census follows uses, not data flow.",
	"A function is a function declaration's body, plus one per package for its variable initializers; a fork in a " +
		"signature, a constant or a type declaration is a fork point outside functions.",
	"Over every build, a share is of the host's own distinct expressions and functions; an expression in a file " +
		"only a build compiles is in no denominator.",
	"Variants count the distinct answers among the configurations holding a thing; a declaration's absence is an " +
		"answer, an expression in a file a build does not compile is not.",
	"Functions the engine's reach marks are not counted here: the census reads programs, not the engine.",
}

// tableText is the census as a table a reader can take in at once.
func tableText(report *censusReport) string {
	output := &strings.Builder{}
	fmt.Fprintf(output, "platform census of %s, patterns %s, tests %v\n", report.Root,
		strings.Join(report.Patterns, " "), report.Tests)
	fmt.Fprintf(output, "workspace: %s\n", report.Workspace)
	fmt.Fprintf(output, "host %s (cgo: %s): %d packages, %d package errors, loaded in %.1fs\n\n",
		report.Host.Target, report.Host.CgoChoice, report.Host.Packages, report.Host.PackageErrors,
		report.Host.LoadSeconds)
	for _, build := range report.Builds {
		fmt.Fprintf(output, "build %s (cgo: %s): %d packages, %d package errors, loaded in %.1fs, compared in %.1fs\n",
			build.Configuration.Target, build.Configuration.CgoChoice, build.Configuration.Packages,
			build.Configuration.PackageErrors, build.Configuration.LoadSeconds, build.Configuration.CompareSeconds)
		rows := [][]string{{"group", "paired", "host only", "build only", "files shared", "files+ (lines)",
			"files- (lines)", "declarations differ", "expressions forked", "by reference", "local", "one side h/b",
			"functions forked", "packages forked"}}
		for _, name := range groupNames {
			group := build.Groups[name]
			rows = append(rows, []string{
				name,
				strconv.Itoa(group.PairedPackages),
				strconv.Itoa(len(group.PackagesOnlyOnHost)),
				strconv.Itoa(len(group.PackagesOnlyInBuild)),
				strconv.Itoa(group.FilesShared),
				fmt.Sprintf("%d (%d)", group.FilesOnlyInBuild.Files, group.FilesOnlyInBuild.Lines),
				fmt.Sprintf("%d (%d)", group.FilesOnlyOnHost.Files, group.FilesOnlyOnHost.Lines),
				ratio(group.DifferingDeclarations, group.Declarations),
				ratio(group.ForkedExpressions, group.AlignedExpressions),
				strconv.Itoa(group.ForkedByReference),
				strconv.Itoa(group.ForkedLocal),
				fmt.Sprintf("%d/%d", group.ExpressionsOnlyOnHost, group.ExpressionsOnlyInBuild),
				ratio(group.ForkedFunctions, group.Functions),
				fmt.Sprintf("%d/%d", group.PackagesWithFork, group.PackagePaths),
			})
		}
		output.WriteString(aligned(rows))
		output.WriteString("\n")
	}
	output.WriteString("over every build, against the host's own\n")
	rows := [][]string{{"group", "expressions forked", "functions forked", "declarations differing",
		"declaration variants", "expression variants"}}
	for _, name := range groupNames {
		overAll := report.OverAll[name]
		rows = append(rows, []string{
			name,
			ratio(overAll.ForkedExpressions, overAll.HostExpressions),
			ratio(overAll.ForkedFunctions, overAll.HostFunctions),
			strconv.Itoa(overAll.DifferingDeclarations),
			histogram(overAll.DeclarationVariants),
			histogram(overAll.ExpressionVariants),
		})
	}
	output.WriteString(aligned(rows))
	return output.String()
}

// aligned spells rows with every column right-aligned to its widest cell, two spaces apart.
func aligned(rows [][]string) string {
	var widths []int
	for _, row := range rows {
		for column, cell := range row {
			if column == len(widths) {
				widths = append(widths, 0)
			}
			widths[column] = max(widths[column], len(cell))
		}
	}
	output := &strings.Builder{}
	for _, row := range rows {
		for column, cell := range row {
			fmt.Fprintf(output, "  %*s", widths[column], cell)
		}
		output.WriteString("\n")
	}
	return output.String()
}

// ratio spells part over whole with its percentage.
func ratio(part int, whole int) string {
	return fmt.Sprintf("%d/%d (%.3f%%)", part, whole, 100*share(part, whole))
}

// histogram spells counts as `answers:how many` pairs, fewest answers first, or `none`.
func histogram(counts map[int]int) string {
	if len(counts) == 0 {
		return "none"
	}
	answers := make([]int, 0, len(counts))
	for count := range counts {
		answers = append(answers, count)
	}
	sort.Ints(answers)
	spelled := make([]string, 0, len(answers))
	for _, count := range answers {
		spelled = append(spelled, fmt.Sprintf("%d:%d", count, counts[count]))
	}
	return strings.Join(spelled, " ")
}
