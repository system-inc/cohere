// The ported `handle` bodies of the framework's single-declaration functional utilities.
//
// One function per root, each the Go form of the `handle` and `staticValues` bodies upstream
// registers for it in `utilities.ts` at 4.3.3. Where frameworkutility.go carries what a root reads,
// this file carries what a root emits, and the reading is computed from the emission by
// `PropertySort` rather than named alongside it.
//
// # Why the shape is ported and the value expression is not
//
// A handle body is two things: which declarations come out, and what goes in each one's value. Both
// consumers of this port need the first and neither reads the second. `PropertySort` reads
// `node.property`, counts declarations, and reads a value only when the property is `--tw-sort`,
// where the value is itself a property name. So a value that is not a sort position can be the
// resolved value passed straight through, which is what all 33 of these roots do anyway.
//
// That is what keeps the port small. It is not a shortcut around `color-mix`, `withAlpha` or the
// `--tw-*` chain: none of those change a property name, so none of them are reachable from what the
// consumers ask.
//
// # Every one of these emits exactly one declaration
//
// Measured at the tag rather than assumed from the table: all 33 registrations spell their handle as
// `handle: (value) => [decl(p, value)]` or as the `handle(value) { if (!value) return; return
// [decl(p, value)] }` guard form, and every `staticValues` entry is a single `decl` naming the same
// property its root does. The four guard-form roots need no guard here, because
// `ResolveFunctionalUtilityValue` already declines a resolved empty value before an emitter runs;
// that equivalence was measured when `RequiresValue` was written, found to change no answer, and
// removed.
//
// The static branch is nevertheless a lookup rather than an inheritance, because a static entry
// declaring a different property is a shape upstream permits and this file cannot check by
// inheriting.
package tailwind

// FrameworkEmitter is the Go form of one root's `handle` body plus its `staticValues` map.
//
// It takes the resolved value rather than a raw candidate because that is where upstream's handle
// sits: resolution has already run, the theme has already been consulted, and a static value has
// already been recognised. A branching handler reads the resolved value and branches on it, the way
// upstream's does.
//
// It returns a node list rather than a Reading so that the reading is computed by the same
// `PropertySort` the engine's is, from declarations rather than from a claim about declarations.
type FrameworkEmitter func(resolved ResolvedUtilityValue) []*Node

// declareProperty is the emitter shared by every root in this file.
//
// All 33 handle bodies are `(value) => [decl(property, value)]`, so the port is one closure over the
// property name rather than 33 bodies that differ only in a string. Written as a constructor rather
// than as a `Property` field read at emit time, so that a root whose handle stops having this shape
// is a different constructor here instead of a special case threaded through the table.
func declareProperty(property string) FrameworkEmitter {
	return func(resolved ResolvedUtilityValue) []*Node {
		return []*Node{Declaration(property, resolved.Value)}
	}
}

// frameworkEmitters is the ported body of each root, keyed the way the table is.
//
// Separate from the table rather than a field inside it so the two stay independently readable: the
// table says what a root accepts, this map says what it emits, and a root missing from either is
// caught by TestEveryFrameworkRootHasAnEmitter rather than by a nil dereference at a call site.
var frameworkEmitters = map[string]FrameworkEmitter{
	"align":              declareProperty("vertical-align"),
	"animate":            declareProperty("animation"),
	"bg-position":        declareProperty("background-position"),
	"bg-size":            declareProperty("background-size"),
	"col":                declareProperty("grid-column"),
	"col-end":            declareProperty("grid-column-end"),
	"col-start":          declareProperty("grid-column-start"),
	"columns":            declareProperty("columns"),
	"contain":            declareProperty("contain"),
	"cursor":             declareProperty("cursor"),
	"delay":              declareProperty("transition-delay"),
	"font-features":      declareProperty("font-feature-settings"),
	"grow":               declareProperty("flex-grow"),
	"list":               declareProperty("list-style-type"),
	"list-image":         declareProperty("list-style-image"),
	"mask-position":      declareProperty("mask-position"),
	"mask-radial-at":     declareProperty("--tw-mask-radial-position"),
	"mask-size":          declareProperty("mask-size"),
	"object":             declareProperty("object-position"),
	"opacity":            declareProperty("opacity"),
	"order":              declareProperty("order"),
	"origin":             declareProperty("transform-origin"),
	"outline-offset":     declareProperty("outline-offset"),
	"perspective":        declareProperty("perspective"),
	"perspective-origin": declareProperty("perspective-origin"),
	"row":                declareProperty("grid-row"),
	"row-end":            declareProperty("grid-row-end"),
	"row-start":          declareProperty("grid-row-start"),
	"shrink":             declareProperty("flex-shrink"),
	"tab":                declareProperty("tab-size"),
	"will-change":        declareProperty("will-change"),
	"z":                  declareProperty("z-index"),
	"zoom":               declareProperty("zoom"),
}
