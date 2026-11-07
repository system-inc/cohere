package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
)

// checkGroup proves a group's split moved code without changing it: the multiset of top-level
// declarations, printed without comments, is the same before and after (a grouped type declaration
// counts as one declaration per type, imports are left out), and so is the multiset of comments,
// leaving out comments inside import declarations and build lines, which new files copy.
func checkGroup(group *packageGroup, outputs map[string][]byte) error {
	before := map[string][]byte{}
	for _, file := range group.files {
		before[file.name] = file.source
	}
	after := map[string][]byte{}
	for name, source := range before {
		after[name] = source
	}
	for name, content := range outputs {
		if content == nil {
			delete(after, name)
		} else {
			after[name] = content
		}
	}
	declarationsBefore, declarationMultisetError := declarationMultiset(before)
	if declarationMultisetError != nil {
		return declarationMultisetError
	}
	declarationsAfter, declarationMultisetError := declarationMultiset(after)
	if declarationMultisetError != nil {
		return declarationMultisetError
	}
	if difference := multisetDifference(declarationsBefore, declarationsAfter); difference != "" {
		return fmt.Errorf("declarations differ: %s", difference)
	}
	commentsBefore, commentMultisetError := commentMultiset(before)
	if commentMultisetError != nil {
		return commentMultisetError
	}
	commentsAfter, commentMultisetError := commentMultiset(after)
	if commentMultisetError != nil {
		return commentMultisetError
	}
	if difference := multisetDifference(commentsBefore, commentsAfter); difference != "" {
		return fmt.Errorf("comments differ: %s", difference)
	}
	return nil
}

func declarationMultiset(files map[string][]byte) (map[string]int, error) {
	multiset := map[string]int{}
	for name, source := range files {
		fileSet := token.NewFileSet()
		syntax, parseFileError := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
		if parseFileError != nil {
			return nil, parseFileError
		}
		for _, declaration := range syntax.Decls {
			nodes := []ast.Node{declaration}
			if general, ok := declaration.(*ast.GenDecl); ok {
				switch general.Tok {
				case token.IMPORT:
					continue
				case token.TYPE:
					nodes = nil
					for _, spec := range general.Specs {
						nodes = append(nodes, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}})
					}
				}
			}
			for _, node := range nodes {
				var printed bytes.Buffer
				if fprintError := printer.Fprint(&printed, fileSet, node); fprintError != nil {
					return nil, fprintError
				}
				multiset[printed.String()]++
			}
		}
	}
	return multiset, nil
}

func commentMultiset(files map[string][]byte) (map[string]int, error) {
	multiset := map[string]int{}
	for name, source := range files {
		syntax, parseFileError := parser.ParseFile(
			token.NewFileSet(),
			name,
			source,
			parser.ParseComments|parser.SkipObjectResolution,
		)
		if parseFileError != nil {
			return nil, parseFileError
		}
		var importSpans [][2]token.Pos
		for _, declaration := range syntax.Decls {
			if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.IMPORT {
				importSpans = append(importSpans, [2]token.Pos{general.Pos(), general.End()})
			}
		}
		for _, group := range syntax.Comments {
			for _, comment := range group.List {
				if comment.Pos() < syntax.Package && isBuildLine(comment.Text) {
					continue
				}
				inImport := false
				for _, span := range importSpans {
					if comment.Pos() >= span[0] && comment.Pos() < span[1] {
						inImport = true
					}
				}
				if !inImport {
					multiset[comment.Text]++
				}
			}
		}
	}
	return multiset, nil
}

// multisetDifference describes the first few entries whose counts differ, or is empty.
func multisetDifference(before map[string]int, after map[string]int) string {
	var differences []string
	for key, count := range before {
		if after[key] != count {
			differences = append(differences, fmt.Sprintf("%d before, %d after: %.120q", count, after[key], key))
		}
	}
	for key, count := range after {
		if _, ok := before[key]; !ok {
			differences = append(differences, fmt.Sprintf("0 before, %d after: %.120q", count, key))
		}
	}
	sort.Strings(differences)
	if len(differences) > 3 {
		differences = append(differences[:3], fmt.Sprintf("and %d more", len(differences)-3))
	}
	if len(differences) == 0 {
		return ""
	}
	return fmt.Sprint(differences)
}
