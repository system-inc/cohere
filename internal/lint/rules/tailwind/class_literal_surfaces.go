package tailwind

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ClassLiteralSurfaces is one set of settings compiled for the run: the unbound reader each file binds,
// and the file-cache key it binds under.
//
// The settings depend on the configuration and never on the file, yet every rule used to build them
// on every file it read, the defaults and then their key: on an ahra cold run 17 MB in 193K objects
// for the defaults and 24 MB in 263K for the keys (#y2nj5ex). Now a rule's decoder compiles them once
// per configuration selection, and a rule reads a pointer.
type ClassLiteralSurfaces struct {
	compiled *ClassLiteralReader
	cacheKey string
}

// ReaderFor returns the reader for these surfaces in this file, shared through the file's cache by
// every rule that reads with the same settings.
//
// Thirteen rules listen on the same kinds, so before this each class surface was read thirteen
// times per file, and each rule compiled its own copy of the variable patterns on every file:
// 13 × 3,978 files × 2 patterns in ahra, about 0.08s of CPU spent recompiling two regular
// expressions. Reading is a pure function of the node and the settings, so one reading serves them
// all. A rule configured with different surfaces gets its own reader, keyed apart.
//
// Under --timing the first rule to reach a node pays for reading it and the rest read the memo, so
// the family's total is the honest number and one rule's row carries the shared reading.
//
// A nil cache yields a reader bound to nothing shared, which still memoizes within the one rule.
func (surfaces *ClassLiteralSurfaces) ReaderFor(cache *rule.FileCache) *ClassLiteralReader {
	return rule.Cached(cache, surfaces.cacheKey, surfaces.bind)
}

// bind is a copy of the compiled reader with a memo of its own, for one file.
func (surfaces *ClassLiteralSurfaces) bind() *ClassLiteralReader {
	bound := *surfaces.compiled
	bound.values = map[*ast.Node]classValues{}
	return &bound
}
