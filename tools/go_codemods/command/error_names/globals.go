package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/naming/go_origin_names"
	"github.com/system-inc/cohere/internal/streams"
)

// The second phase renames the declarations whose name abbreviates error and whose uses reach past one
// function: package-level sentinels, which keep Go's err prefix spelled out as staticcheck's ST1012 asks
// (`errPoolNeverEmpty` gives errorPoolNeverEmpty), error-making functions (`errNothingToRepeat` gives
// nothingToRepeatError), and fields (`wantErr` gives wantError, a field `Err` gives Error). They go through
// the cross-package engine, which keeps a name others own: an exported name used outside the workspace, a
// method an interface needs, a field an encoder reads by name. A declaration another finding quotes is
// renamed too, and that finding re-worded under the new name.

// globalErrorName is the new name of a declaration that is not a local, or false when its name does not
// abbreviate error. A package-level variable opening on err keeps the prefix, spelled out. An exported name
// opening on Err followed by more words is Go's own convention for a sentinel, which the house rule leaves
// alone, so it is kept.
func globalErrorName(name string, packageVariable bool, words go_origin_names.Vocabulary) (string, bool) {
	if name == "Err" {
		return "Error", true
	}
	parts := go_origin_names.CamelWords(name)
	if len(parts) > 1 && parts[0] == "Err" {
		return "", false
	}
	if packageVariable && len(parts) > 1 && parts[0] == "err" {
		var builder strings.Builder
		builder.WriteString("error")
		for index, word := range parts[1:] {
			if expansion, abbreviated := words.Expansions[strings.ToLower(word)]; abbreviated {
				word = strings.ToUpper(expansion[:1]) + expansion[1:]
			}
			builder.WriteString(go_origin_names.HouseCased(word, index+1))
		}
		return builder.String(), true
	}
	newName, category, abbreviated := abbreviatedErrorName(name, words)
	if !abbreviated || category != "" {
		return "", false
	}
	return newName, true
}

// findGlobalProposals lists every package-level declaration and field of the editable files whose name
// abbreviates error, once each.
func findGlobalProposals(loaded *workspace.Workspace, words go_origin_names.Vocabulary) []global_rename.Proposal {
	var proposals []global_rename.Proposal
	seen := map[string]bool{}
	for _, analyzedPackage := range loaded.Analyzed {
		for _, file := range loaded.RenamePackage(analyzedPackage).Files {
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				object := analyzedPackage.TypesInfo.Defs[identifier]
				if object == nil || object.Pkg() != analyzedPackage.Types {
					return true
				}
				variable, isVariable := object.(*types.Var)
				packageLevel := object.Parent() == analyzedPackage.Types.Scope()
				if !packageLevel && !(isVariable && variable.IsField()) {
					return true
				}
				newName, abbreviated := globalErrorName(object.Name(), packageLevel && isVariable, words)
				if !abbreviated {
					return true
				}
				position := analyzedPackage.Fset.PositionFor(object.Pos(), false)
				key := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
				if seen[key] {
					return true
				}
				seen[key] = true
				proposals = append(
					proposals,
					global_rename.Proposal{Package: analyzedPackage, Object: object, NewName: newName},
				)
				return true
			})
		}
	}
	return proposals
}

// planGlobals plans the second phase over loaded and prints what it decided.
func planGlobals(loaded *workspace.Workspace, words go_origin_names.Vocabulary) *global_rename.Result {
	proposals := findGlobalProposals(loaded, words)
	result := global_rename.Plan(loaded, proposals, global_rename.Options{})
	fmt.Fprintf(
		streams.Output,
		"\nsecond phase, declarations past one function whose name abbreviates error: %d proposed, %d accepted, %d refused\n",
		len(proposals),
		len(result.Accepted),
		len(result.Rejections),
	)
	byClass := map[string]int{}
	for _, rejection := range result.Rejections {
		byClass[rejection.Class]++
	}
	printCounts(byClass, 0)
	accepted := append([]global_rename.Rename(nil), result.Accepted...)
	sort.Slice(accepted, func(left, right int) bool {
		return accepted[left].Position.String() < accepted[right].Position.String()
	})
	fmt.Fprintln(streams.Output, "accepted:")
	for _, each := range accepted {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s\n",
			relativePosition(loaded, each.Position),
			each.Kind,
			each.OldName,
			each.NewName,
		)
	}
	fmt.Fprintln(streams.Output, "refused:")
	for _, rejection := range result.Rejections {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s: %s\n",
			relativePosition(loaded, rejection.Position),
			rejection.Kind,
			rejection.OldName,
			rejection.NewName,
			strings.ReplaceAll(rejection.Reason, loaded.Root+"/", ""),
		)
	}
	for _, check := range [][]string{
		result.Unresolved,
		result.SelectionModelMismatches,
		result.SatisfactionModelMismatches,
	} {
		for _, entry := range check {
			fmt.Fprintln(streams.Output, "  model check: "+strings.ReplaceAll(entry, loaded.Root+"/", ""))
		}
	}
	return result
}

// applyGlobals reloads the tree the first phase wrote, plans the second phase over it, and applies it. It
// returns the exit code.
func applyGlobals(root string, skips []string, words go_origin_names.Vocabulary) int {
	reloaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "%s: reloading for the second phase: %v\n", commandName, loadError)
		return 1
	}
	result := planGlobals(reloaded, words)
	if len(result.Accepted) == 0 {
		return 0
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(streams.Output)
	mismatches, applyError := global_rename.Apply(reloaded, root, skips, result, streams.Output)
	if applyError != nil {
		fmt.Fprintf(streams.Errors, "%s: second phase: %v\n", commandName, applyError)
		return 1
	}
	if mismatches > 0 {
		fmt.Fprintf(streams.Errors, "%s: second phase: %d mismatches after writing\n", commandName, mismatches)
		return 1
	}
	return 0
}
