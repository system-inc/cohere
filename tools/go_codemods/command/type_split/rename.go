package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"sort"
	"strings"
)

// fileMove is a file's content leaving one name for another: a rename, a merge into an existing file, a file a
// split made out of another, or a file a split emptied into the one that took its code. renames.tsv lists them so
// the gate follows findings across the move.
type fileMove struct {
	from string
	to   string
	// whole is whether from is gone after the move, renamed, merged or emptied, rather than one more file made out
	// of it.
	whole bool
	// continues is whether to is from under another name, renamed or merged into, so a mention of from means to,
	// where a file the split emptied into several has no one file a mention means.
	continues bool
}

// planRenames names each file of groups the file name rule flags for the one type it serves, after the split.
// A file goes to the name the rule suggests when no file of the directory has it. When the name is taken by a file
// of the same package that the merge would leave serving the one type, built the same way and within lineLimit,
// the file's code joins that file. A file of only a type's methods whose type's name is taken otherwise goes to a
// themed name, the type's and then its own, which the rule accepts for such a file. Anything else is a conflict,
// listed and left alone. taken holds every name of the directory in lowercase and gains each name the plan takes.
// Mutates: taken each name the plan takes joins the directory's names, so a later file finds it taken
// Mutates: result the directory's renames, merges and conflicts gather every file's plan
func planRenames(directory string, groups []*packageGroup, taken map[string]bool, around surroundings,
	result *directoryResult) renamePlan {
	plan := renamePlan{changes: map[string][]byte{}}
	for _, group := range groups {
		filesByName := map[string]*sourceFile{}
		for _, file := range group.files {
			filesByName[file.name] = file
		}
		parts := partsOf(group)
		misnamedList := misnamedFiles(group)
		// mergedInto is the files a merge has already changed in this pass, and every misnamed file, which this
		// pass moves: nothing joins either, and the next round sees the result.
		mergedInto := map[string]bool{}
		for _, misnamed := range misnamedList {
			mergedInto[misnamed.file.name] = true
		}
		for _, misnamed := range misnamedList {
			file := misnamed.file
			// A minority type is extracted by the split, in this round or the next; only a file whose type
			// could not leave is renamed for it.
			if misnamed.extractable() && !result.extractionsTried[file.name] {
				continue
			}
			if file.pinned != "" {
				result.nameConflicts = append(result.nameConflicts, fmt.Sprintf("%s/%s: pinned (%s), stays for %s",
					directory, file.name, file.pinned, misnamed.typeName))
				continue
			}
			if misnamed.extractable() && misnamed.namesOtherDeclaration() {
				result.nameConflicts = append(result.nameConflicts, fmt.Sprintf("%s/%s: the file is named for its "+
					"package or another of its declarations and its type could not leave, stays (%s)", directory,
					file.name, misnamed.kind()))
				continue
			}
			if misnamed.nameInCode {
				result.nameConflicts = append(result.nameConflicts, fmt.Sprintf("%s/%s: Go code reads the file by "+
					"this name and its type could not leave, stays (%s)", directory, file.name, misnamed.kind()))
				continue
			}
			// A file of only the type's methods keeps a theme, so the type's own name stays for the file declaring
			// it; one with no theme apart from the type joins the type's file.
			target := misnamed.suggestion
			if !misnamed.declared {
				if themed := themedName(misnamed.typeName, file.name); themed != "" {
					target = themed
				}
			}
			if problem := renameProblem(directory, file, target, around); problem != "" {
				result.nameConflicts = append(result.nameConflicts, fmt.Sprintf("%s/%s: %s (%s)",
					directory, file.name, problem, misnamed.kind()))
				continue
			}
			if !taken[strings.ToLower(target)] && (misnamed.declared || target != misnamed.suggestion) {
				taken[strings.ToLower(target)] = true
				plan.rename(file, target)
				result.renamed = append(result.renamed, fmt.Sprintf("%s/%s -> %s (%s)", directory, file.name, target,
					misnamed.kind()))
				continue
			}
			mergeTarget := misnamed.suggestion
			if !misnamed.declared {
				mergeTarget = declarationFileOf(group, misnamed.typeName)
			}
			home := filesByName[mergeTarget]
			merged, mergeProblem := mergeFile(group, file, home, parts, mergedInto, around)
			if mergeProblem == "" {
				mergedInto[mergeTarget] = true
				plan.changes[mergeTarget] = merged
				plan.changes[file.name] = nil
				plan.moves = append(
					plan.moves,
					fileMove{from: file.name, to: mergeTarget, whole: true, continues: true},
				)
				result.merged = append(result.merged, fmt.Sprintf("%s/%s -> %s (%s)", directory, file.name,
					mergeTarget, misnamed.kind()))
				continue
			}
			result.nameConflicts = append(result.nameConflicts, fmt.Sprintf("%s/%s: %s is taken, and %s cannot take "+
				"its code: %s (%s)", directory, file.name, target, mergeTarget, mergeProblem, misnamed.kind()))
		}
	}
	return plan
}

// renameProblem says why file cannot take name, or is empty: the go command must build it exactly as before, so
// the name keeps the toolchain suffix and test suffix it has and adds none, and it may not start with a dot or an
// underscore.
func renameProblem(directory string, file *sourceFile, name string, around surroundings) string {
	_, suffix := fileNameParts(file.name)
	_, newSuffix := fileNameParts(name)
	switch {
	case strings.HasPrefix(name, "_") || strings.HasPrefix(name, "."):
		return name + " would be ignored by the go command"
	case suffix != newSuffix:
		return name + " would carry other build constraints"
	case around.builds(directory, name, file.source) != file.built:
		return name + " would be built differently"
	}
	return ""
}

// themedName is the name of a file of only typeName's methods that keeps its own name as a theme: graph_walk.go for
// Graph's methods in walk.go, with the toolchain and test suffix fileName carries. A word of the theme that echoes a
// word of the type's name is dropped, so Canonicalizer's methods in canonicalize_variants.go go to
// canonicalizer_variants.go, not canonicalizer_canonicalize_variants.go. It is empty when no word is left.
func themedName(typeName string, fileName string) string {
	base, suffix := fileNameParts(fileName)
	typeBase := snakeCase(typeName)
	typeWords := strings.Split(typeBase, "_")
	var theme []string
	for _, word := range strings.Split(base, "_") {
		echoes := word == ""
		for _, typeWord := range typeWords {
			if wordsEcho(word, typeWord) {
				echoes = true
			}
		}
		if !echoes {
			theme = append(theme, word)
		}
	}
	if len(theme) == 0 {
		return ""
	}
	return typeBase + "_" + strings.Join(theme, "_") + suffix + ".go"
}

// wordsEcho is whether two words of a name are one word in two forms: equal, or one a prefix of the other at
// least four letters long, as walk and walker or canonicalize and canonicalizer.
func wordsEcho(left string, right string) bool {
	if left == right {
		return true
	}
	shorter, longer := left, right
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	return len(shorter) >= 4 && strings.HasPrefix(longer, shorter)
}

// mergeFile is home with file's code appended, or why it cannot take it: home must be a file of the same package,
// judged, not pinned, built under the same constraints, merged into by nothing else in this pass, and serving at
// most the one type file serves, and file may carry nothing before its package clause but build lines, since a
// package comment has no place in the middle of another file. The result must stay within lineLimit and pass the
// equivalence check.
func mergeFile(group *packageGroup, file *sourceFile, home *sourceFile, parts map[*sourceFile][]*part,
	mergedInto map[string]bool, around surroundings) ([]byte, string) {
	switch {
	case home == nil:
		return nil, "it is not a file of the same package"
	case !home.judged || home.pinned != "":
		return nil, "it is pinned or not judged"
	case home.key != file.key:
		return nil, "it is built under other constraints"
	case mergedInto[home.name]:
		return nil, "it already took another file or is renamed itself this round"
	}
	for _, piece := range parts[home] {
		if piece.name != parts[file][0].name {
			return nil, "it serves " + piece.name
		}
	}
	for _, commentGroup := range file.syntax.Comments {
		if commentGroup.Pos() >= file.syntax.Package {
			break
		}
		for _, comment := range commentGroup.List {
			if !isBuildLine(comment.Text) {
				return nil, "the file carries a comment before its package clause"
			}
		}
	}
	// The body is everything after the package clause but the imports, free comments included, so the merge moves
	// the file's code whole.
	bodyStart := file.lineRange(file.syntax.Package, file.syntax.Name.End()).end
	var importRanges []byteRange
	for _, declaration := range file.syntax.Decls {
		if general, isGeneral := declaration.(*ast.GenDecl); isGeneral && general.Tok == token.IMPORT {
			span := file.lineRange(docStart(general.Doc, general.Pos()), general.End())
			importRanges = append(importRanges, byteRange{start: span.start - bodyStart, end: span.end - bodyStart})
		}
	}
	body, spliceError := splice(file.source[bodyStart:], importRanges)
	if spliceError != nil {
		return nil, spliceError.Error()
	}
	text := append(bytes.TrimRight(append([]byte(nil), home.source...), "\n"), '\n', '\n')
	text = append(text, bytes.TrimLeft(body, "\n")...)
	fixed, fixError := fixImports(text, []*sourceFile{file, home}, around.importName)
	if fixError != nil {
		return nil, fixError.Error()
	}
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		return nil, formatError.Error()
	}
	if lines := bytes.Count(formatted, []byte("\n")); lines > lineLimit {
		return nil, fmt.Sprintf("the merge would hold %d lines", lines)
	}
	if checkError := checkGroup(group, map[string][]byte{home.name: formatted, file.name: nil}); checkError != nil {
		return nil, checkError.Error()
	}
	return formatted, ""
}

// sortedNames is the keys of changes in order.
func sortedNames(changes map[string][]byte) []string {
	names := make([]string, 0, len(changes))
	for name := range changes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// declarationFileOf is the name of the file of group that declares typeName, or empty.
func declarationFileOf(group *packageGroup, typeName string) string {
	for _, record := range typesOf(group) {
		if record.name == typeName {
			return record.file.name
		}
	}
	return ""
}
