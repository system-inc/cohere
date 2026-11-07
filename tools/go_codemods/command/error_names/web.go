package main

import (
	"go/ast"
	"go/token"
)

// web is one set of definitions and the reads they reach.
type web struct {
	definitions []definitionSite
	uses        []*ast.Ident
	// newName is the name its definitions give it, or "" with a category when they give none.
	newName  string
	category string
	// neutral marks a web written only by its declaration's zero value or nil, which names nothing.
	neutral bool
	// calls lists, for the report, the calls feeding a web that two names feed.
	calls []string
}

// first is the web's earliest position, definition or read.
func (each *web) first() token.Pos {
	first := token.NoPos
	for _, definition := range each.definitions {
		if first == token.NoPos || definition.identifier.Pos() < first {
			first = definition.identifier.Pos()
		}
	}
	for _, use := range each.uses {
		if first == token.NoPos || use.Pos() < first {
			first = use.Pos()
		}
	}
	return first
}
