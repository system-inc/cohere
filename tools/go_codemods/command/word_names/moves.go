package main

// A file named for a type moves with the type. The house names a file that serves one type with methods for that
// type in snake_case (cohere-go/consistency-require-matching-file-name), so renaming oneVarChecker leaves
// one_var_checker.go naming a type that no longer exists, and the rule flags it. The pass moves such a file to the
// new type's name, keeping any tail after the type's part, a toolchain suffix or a theme (registry_unix.go,
// graph_walk.go), and moves the file's test beside it. A move whose target exists is refused and listed; the rename
// itself stands.

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// fileMove is one file the pass moves, or would move but for refusal.
type fileMove struct {
	oldPath string
	newPath string
	// typeName is the renamed type the file is named for, as old -> new.
	typeName string
	refusal  string
}

// planFileMoves finds, for each accepted type rename, every non-test file declaring the type or one of its methods
// whose name spells the old type, and its test file beside it.
func planFileMoves(
	loaded *workspace.Workspace,
	result *global_rename.Result,
	proposals []global_rename.Proposal,
) []fileMove {
	objects := map[string]types.Object{}
	for _, proposal := range proposals {
		position := proposal.Package.Fset.PositionFor(proposal.Object.Pos(), false)
		objects[fmt.Sprintf("%s:%d", position.Filename, position.Offset)] = proposal.Object
	}
	fileSet := loaded.Roots[0].Fset
	var moves []fileMove
	planned := map[string]bool{}
	for _, renamed := range result.Accepted {
		if renamed.Kind != global_rename.KindType {
			continue
		}
		typeName, isTypeName := objects[fmt.Sprintf(
			"%s:%d",
			renamed.Position.Filename,
			renamed.Position.Offset,
		)].(*types.TypeName)
		if !isTypeName {
			continue
		}
		files := map[string]bool{renamed.Position.Filename: true}
		if named, isNamed := typeName.Type().(*types.Named); isNamed {
			for index := range named.NumMethods() {
				files[fileSet.PositionFor(named.Method(index).Pos(), false).Filename] = true
			}
		}
		for _, filename := range sortedKeys(files) {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") || !strings.HasSuffix(base, ".go") || planned[filename] {
				continue
			}
			stem := strings.TrimSuffix(base, ".go")
			tail, named := tailAfterType(stem, renamed.OldName)
			if !named {
				continue
			}
			newStem := snakeCase(renamed.NewName) + tail
			for _, pair := range [][2]string{
				{stem + ".go", newStem + ".go"},
				{stem + "_test.go", newStem + "_test.go"},
			} {
				oldPath := filepath.Join(filepath.Dir(filename), pair[0])
				if _, statError := os.Stat(oldPath); statError != nil {
					continue
				}
				move := fileMove{oldPath: oldPath, newPath: filepath.Join(filepath.Dir(filename), pair[1]),
					typeName: renamed.OldName + " -> " + renamed.NewName}
				if _, statError := os.Stat(move.newPath); statError == nil {
					move.refusal = "the new name is taken"
				}
				planned[oldPath] = true
				moves = append(moves, move)
			}
		}
	}
	sort.Slice(moves, func(left, right int) bool { return moves[left].oldPath < moves[right].oldPath })
	return moves
}

// tailAfterType is what a file's stem holds after the part that spells typeName, compared as the rule compares,
// without case or underscores: "" for the whole stem, "_unix" for registry_unix, and whether the stem spells it.
func tailAfterType(stem string, typeName string) (string, bool) {
	typeKey := spellingKey(typeName)
	if spellingKey(stem) == typeKey {
		return "", true
	}
	for index, character := range stem {
		if character == '_' && index+1 < len(stem) && spellingKey(stem[:index]) == typeKey {
			return stem[index:], true
		}
	}
	return "", false
}

// spellingKey and snakeCase are the rule's own, internal/goengine/rules/consistency_require_matching_file_name, kept
// to their letter so a moved file is one the rule accepts.
func spellingKey(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '_' {
			return -1
		}
		return unicode.ToLower(character)
	}, text)
}

func snakeCase(name string) string {
	runes := []rune(name)
	var builder strings.Builder
	for index, character := range runes {
		if index > 0 && unicode.IsUpper(character) {
			previous := runes[index-1]
			lowerFollows := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && lowerFollows) {
				builder.WriteRune('_')
			}
		}
		builder.WriteRune(unicode.ToLower(character))
	}
	return builder.String()
}

// moveFiles moves each planned file that no refusal holds back, after the rename is written.
func moveFiles(moves []fileMove) error {
	for _, move := range moves {
		if move.refusal != "" {
			continue
		}
		if renameError := os.Rename(move.oldPath, move.newPath); renameError != nil {
			return fmt.Errorf("moving %s: %w", move.oldPath, renameError)
		}
	}
	return nil
}
