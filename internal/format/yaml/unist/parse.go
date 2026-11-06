package unist

// Ported from yaml-unist-parser 3.2.0, dist/parse.mjs and dist/yaml-syntax-error.mjs.

import (
	"fmt"
	"runtime"
	"slices"
	"sort"
	"unicode/utf16"

	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/yaml/compose"
	"github.com/system-inc/cohere/internal/format/yaml/cst"
)

// Parse is yaml-unist-parser's parse(text, { uniqueKeys: false }), the call Prettier's parser-yaml.js
// makes: eemeli/yaml's parser and composer, the transforms into the unist tree, attachComments (which
// defines every Parent) and updatePositions. Offsets are converted to bytes once, at the end.
//
// A document with an error fails the parse with a *SyntaxError, upstream's YAMLSyntaxError. Where
// upstream throws anything else (its own invariant checks, or a TypeError on a shape it does not
// expect), Parse returns a *ThrownError with the same name and message. A panic in the port itself is
// returned as a plain error.
//
// The root keeps Comments, every comment in the file, as upstream's root does; Prettier's parser deletes
// root.comments before printing, and the printer never reads it.
//
// The nodes come from nodes, which may be nil; the tree is valid until it is Reset.
func Parse(text string, nodes *arena.Arena[Node]) (root *Node, parseError error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if thrown, isThrown := recovered.(*ThrownError); isThrown {
				root, parseError = nil, thrown
				return
			}
			if runtimeError, isRuntimeError := recovered.(runtime.Error); isRuntimeError {
				root, parseError = nil, fmt.Errorf("unist: %w", runtimeError)
				return
			}
			root, parseError = nil, fmt.Errorf("unist: %v", recovered)
		}
	}()

	memory, isParseMemory := parseMemories.Get().(*parseMemory)
	if !isParseMemory {
		// Invariant: the pool holds only what its New makes and Parse puts back, both *parseMemory.
		panic("unist: parseMemories held a value that is not a parse memory")
	}
	defer func() {
		memory.reset()
		parseMemories.Put(memory)
	}()

	// A JavaScript string: UTF-16 units. An invalid UTF-8 byte is one U+FFFD, as newUnitOffsets counts.
	// Both tables are the memory's, kept across parses: nothing past Parse reads either, since node values
	// are copied out as strings and positions are converted into the nodes before Parse returns.
	memory.units = appendUnits(memory.units[:0], text)
	units := memory.units
	memory.offsets = newUnitOffsets(text, memory.offsets[:0])
	offsets := memory.offsets

	lineCounter := cst.NewLineCounter()
	context := newContext(units, lineCounter, nodes, memory)
	parser := cst.NewParser(lineCounter.AddNewLine)
	parser.Tokens = &memory.tokens
	composeOptions := compose.UnistParserOptions()
	composeOptions.Nodes = &memory.composeNodes
	composer := compose.NewComposer(composeOptions)
	parsedDocuments := []*compose.Document{}
	cstTokens := slices.Collect(parser.Parse(units, false))
	for parsedDocument := range composer.Compose(cstTokens, true, len(units)) {
		if len(parsedDocument.Errors) > 0 {
			return nil, context.newYamlSyntaxError(parsedDocument.Errors[0], offsets)
		}
		parsedDocuments = append(parsedDocuments, parsedDocument)
	}

	documents := context.transformDocuments(parsedDocuments, cstTokens)
	// `context.comments.sort((commentA, commentB) => commentA.position.start.offset -
	// commentB.position.end.offset)`. Comments never overlap, and each ends after it starts, so for two
	// different comments the comparator says the earlier one goes first either way round: it orders them
	// by start offset, which any sort reproduces.
	comments := context.comments
	sort.SliceStable(comments, func(left int, right int) bool {
		return context.position(comments[left]).start.offset < context.position(comments[right]).start.offset
	})
	root = context.createRoot(context.transformRange(0, len(units)), documents, comments)
	context.attachComments(root)
	context.updatePositions(root)

	// Every node was made by a factory, which registered its position object.
	for node, position := range context.positions {
		node.Position = offsets.convert(position)
	}
	return root, nil
}

// newUnitOffsets appends text's offsets to offsets, which may be a table kept from an earlier parse.
func newUnitOffsets(text string, offsets unitOffsets) unitOffsets {
	table := slices.Grow(offsets, len(text)+1)
	for index, character := range text {
		table = append(table, index)
		if character > 0xFFFF {
			// The low surrogate of a pair sits at the pair's start too; no position upstream records
			// falls between the two units of one character.
			table = append(table, index)
		}
	}
	return append(table, len(text))
}

// appendUnits appends text as UTF-16 units to units: utf16.Encode([]rune(text)), into a table kept from an
// earlier parse. Ranging a string decodes an invalid byte to U+FFFD, as the conversion to runes does.
func appendUnits(units []uint16, text string) []uint16 {
	appended := slices.Grow(units, len(text))
	for _, character := range text {
		appended = utf16.AppendRune(appended, character)
	}
	return appended
}
