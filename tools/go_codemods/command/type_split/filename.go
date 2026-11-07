package main

import (
	"go/ast"
	"go/build/constraint"
	"go/token"
	"strings"
	"unicode"
)

// The functions here mirror cohere's internal/goengine/rules/consistency_require_matching_file_name, which a module
// outside cohere cannot import, so the files typesplit names are the ones that rule accepts and the files it renames
// are exactly the ones that rule flags. operatingSystems and architectures in names.go are the same lists the rule
// copies from go/build.

// declarationLineCounts is the lines file's declarations take with their docs: those of typeName, the type's
// declaration and methods, and those of every declaration but the imports.
func declarationLineCounts(file *sourceFile, typeName string, aliasOf func(string) string) (typeLines int,
	declarationLines int) {
	span := func(doc *ast.CommentGroup, start token.Pos, end token.Pos) int {
		return file.line(end) - file.line(docStart(doc, start)) + 1
	}
	for _, declaration := range file.syntax.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			lines := span(node.Doc, node.Pos(), node.End())
			declarationLines += lines
			if node.Recv != nil && aliasOf(receiverTypeName(node)) == typeName {
				typeLines += lines
			}
		case *ast.GenDecl:
			if node.Tok == token.IMPORT {
				continue
			}
			declarationLines += span(node.Doc, node.Pos(), node.End())
			for _, spec := range node.Specs {
				if typeSpec, isTypeSpec := spec.(*ast.TypeSpec); isTypeSpec && typeSpec.Name.Name == typeName &&
					typeSpec.Assign == token.NoPos {
					doc := typeSpec.Doc
					if node.Lparen == token.NoPos {
						doc = node.Doc
					}
					typeLines += span(doc, typeSpec.Pos(), typeSpec.End())
				}
			}
		}
	}
	return typeLines, declarationLines
}

// topLevelNames is every name a file declares at the top level but its methods and imports.
func topLevelNames(syntax *ast.File) []string {
	var names []string
	for _, declaration := range syntax.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			if node.Recv == nil {
				names = append(names, node.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch typed := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, typed.Name.Name)
				case *ast.ValueSpec:
					for _, name := range typed.Names {
						names = append(names, name.Name)
					}
				}
			}
		}
	}
	return names
}

// misnamedFiles is every file of group the file name rule flags, in file order. A file the rules do not judge, a
// test file, main.go in package main, and a file serving no type or several are left out, as the rule leaves them.
func misnamedFiles(group *packageGroup) []misnamedFile {
	var misnamed []misnamedFile
	parts := partsOf(group)
	for _, file := range group.files {
		if !file.judged || strings.HasSuffix(file.name, "_test.go") {
			continue
		}
		if file.name == "main.go" && group.packageName == "main" {
			continue
		}
		if len(parts[file]) != 1 {
			continue
		}
		only := parts[file][0]
		stem := fileStem(file.name)
		buildConstrained := hasBuildConstraint(file.syntax)
		allowedTail := isToolchainSuffix
		if buildConstrained {
			allowedTail = isVariantSuffix
		}
		if !only.declares {
			allowedTail = isThemeSuffix
		}
		if namesType(stem, only.name, allowedTail) {
			continue
		}
		typeLines, declarationLines := declarationLineCounts(file, only.name, methodOwner(group))
		misnamed = append(misnamed, misnamedFile{
			file:       file,
			typeName:   only.name,
			declared:   only.declares,
			suggestion: snakeCase(only.name) + variantSuffix(stem, buildConstrained) + ".go",

			part:             only,
			nameInCode:       group.namedInCode[file.name],
			typeLines:        typeLines,
			declarationLines: declarationLines,
		})
	}
	return misnamed
}

// fileStem is a file's name up to its first dot, the part go/build reads a toolchain suffix from.
func fileStem(fileName string) string {
	stem, _, _ := strings.Cut(fileName, ".")
	return stem
}

// namesType is whether stem spells typeName, alone or followed by an underscore and a tail allowedTail accepts,
// compared without case or underscores.
func namesType(stem string, typeName string, allowedTail func(string) bool) bool {
	typeKey := spellingKey(typeName)
	if spellingKey(stem) == typeKey {
		return true
	}
	for index, character := range stem {
		if character != '_' {
			continue
		}
		head, tail := stem[:index], stem[index+1:]
		if tail != "" && spellingKey(head) == typeKey && allowedTail(tail) {
			return true
		}
	}
	return false
}

// spellingKey is text in lowercase with its underscores dropped.
func spellingKey(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '_' {
			return -1
		}
		return unicode.ToLower(character)
	}, text)
}

// isToolchainSuffix is whether tail is one go/build reads as a build constraint: an operating system, an
// architecture, or an operating system and then an architecture.
func isToolchainSuffix(tail string) bool {
	parts := strings.Split(tail, "_")
	switch len(parts) {
	case 1:
		return operatingSystems[parts[0]] || architectures[parts[0]]
	case 2:
		return operatingSystems[parts[0]] && architectures[parts[1]]
	default:
		return false
	}
}

// isVariantSuffix is whether tail names a build-constrained file's variant: a toolchain suffix or one word.
func isVariantSuffix(tail string) bool {
	return isToolchainSuffix(tail) || !strings.Contains(tail, "_")
}

// isThemeSuffix accepts any tail, since a file of one type's methods may be named for their theme.
func isThemeSuffix(string) bool {
	return true
}

// hasBuildConstraint is whether file carries a //go:build line before its package clause.
func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			return false
		}
		for _, comment := range group.List {
			if constraint.IsGoBuild(comment.Text) {
				return true
			}
		}
	}
	return false
}

// toolchainSuffix is the build constraint suffix stem ends with, read as go/build reads it, or empty.
func toolchainSuffix(stem string) string {
	_, rest, found := strings.Cut(stem, "_")
	if !found {
		return ""
	}
	parts := strings.Split(rest, "_")
	count := len(parts)
	if count >= 2 && operatingSystems[parts[count-2]] && architectures[parts[count-1]] {
		return "_" + parts[count-2] + "_" + parts[count-1]
	}
	if operatingSystems[parts[count-1]] || architectures[parts[count-1]] {
		return "_" + parts[count-1]
	}
	return ""
}

// variantSuffix is the suffix a suggested name keeps: the toolchain suffix stem ends with, or else, in a
// build-constrained file, its last word.
func variantSuffix(stem string, buildConstrained bool) string {
	if suffix := toolchainSuffix(stem); suffix != "" || !buildConstrained {
		return suffix
	}
	index := strings.LastIndex(stem, "_")
	if index <= 0 || index == len(stem)-1 {
		return ""
	}
	return stem[index:]
}

// methodOwner resolves a receiver's spelled type name through group's local aliases, as typesOf does.
func methodOwner(group *packageGroup) func(string) string {
	aliasOf := map[string]string{}
	for _, file := range group.files {
		for _, declaration := range file.syntax.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if isTypeSpec && typeSpec.Assign != token.NoPos && file.built {
					aliasOf[typeSpec.Name.Name] = typeExpressionName(typeSpec.Type)
				}
			}
		}
	}
	return func(name string) string {
		return resolveAlias(name, aliasOf)
	}
}
