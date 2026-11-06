package core

import (
	"encoding/json"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// MaxDepthSettings is the decoded option surface: how deeply blocks may nest.
type MaxDepthSettings struct {
	// Maximum is the deepest nesting allowed before a block reports. Upstream's default is 4.
	Maximum int

	// Disabled reproduces the one option shape under which upstream reports nothing at all.
	//
	// `{"maximum": 0}` makes upstream's `option.maximum || option.max` evaluate to `undefined`,
	// and every `depth > undefined` comparison is false, so the rule goes silent rather than
	// reporting every block. Modelled as its own flag because no integer value of Maximum can
	// express it: 0 means "report every block" and there is no number meaning "report none".
	Disabled bool
}

// DefaultMaxDepthSettings is upstream's `defaultOptions: [4]`, spelled out because the zero value
// of the struct is not it: a Maximum of 0 means "no block may nest at all", which is a legal
// configuration and the opposite of the default.
func DefaultMaxDepthSettings() MaxDepthSettings {
	return MaxDepthSettings{Maximum: 4}
}

// DecodeMaxDepthOptions reads the depth limit off the config.
//
// Hand rolled because upstream's schema is a `oneOf`: the option is either a bare integer or an
// object carrying `maximum` or `max`, and `rule.DecodeOptionsInto` expresses neither alternative.
// The corpus configures all three spellings, so all three are accepted:
//
//	["error", 2]                the bare integer
//	["error", { "maximum": 2 }]
//	["error", { "max": 2 }]     an older alias upstream still honours
//
// # The object form has a JavaScript quirk in it, and the two zero spellings do opposite things
//
// Upstream's handling is two statements:
//
//	if (typeof option === "object" && (hasOwn(option,"maximum") || hasOwn(option,"max")))
//	    maxDepth = option.maximum || option.max;
//
// The `hasOwn` guard decides whether the branch is entered; the `||` inside decides the value, and
// it is a truthiness pick rather than a presence one. Those two tests disagree exactly when a zero
// is written, and the result is not what either reading alone predicts. Measured against the
// installed rule, reading the limit out of its own message rather than inferring it from a count:
//
//	2                       limit 2      the ordinary object-free spelling
//	0                       limit 0      a bare zero reports every block
//	{"max": 0}              limit 0      `undefined || 0` is 0, so zero WINS here
//	{"maximum": 0}          SILENT       `0 || undefined` is undefined, and every
//	                                     `depth > undefined` is false, so the rule reports nothing
//	{"max": 0, "maximum": 3}  limit 3    the `||` consults maximum first
//	{}                      limit 4      neither key present, so the branch is not entered
//
// Both zero spellings are in upstream's corpus. `{"maximum": 0}` silencing the rule entirely is a
// defect rather than a design, but it is a verdict and this port reproduces it: a rule that
// disagreed with the tool it replaces on a configuration somebody wrote would be the worse outcome,
// and the divergence is recorded here rather than silently improved. An earlier draft of this
// decoder read the `||` as a plain truthiness fallback and got both zero rows wrong, which the
// corpus caught.
//
// Empty input decodes to the default rather than failing, which is what a rule configured as a bare
// "error" is handed and what upstream's `defaultOptions` says it means.
func DecodeMaxDepthOptions(raw []byte) (MaxDepthSettings, error) {
	settings := DefaultMaxDepthSettings()
	if len(raw) == 0 {
		return settings, nil
	}

	var maximum int
	if decodeError := json.Unmarshal(raw, &maximum); decodeError == nil {
		settings.Maximum = maximum
		return settings, nil
	}

	var object struct {
		Maximum *int `json:"maximum"`
		Max     *int `json:"max"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return settings, unmarshalOptionsError
	}

	// Upstream's `hasOwn` guard: neither key present means the branch is never entered and the
	// initialized default of 4 stands.
	if object.Maximum == nil && object.Max == nil {
		return settings, nil
	}

	// Upstream's `option.maximum || option.max`, evaluated the way JavaScript evaluates it.
	switch {
	case object.Maximum != nil && *object.Maximum != 0:
		settings.Maximum = *object.Maximum
	case object.Max != nil && *object.Max != 0:
		settings.Maximum = *object.Max
	case object.Max != nil:
		// `undefined || 0` and `0 || 0` both yield 0, so a `max` of zero wins whenever `maximum`
		// is absent or is itself zero.
		settings.Maximum = 0
	default:
		// Only `maximum` was written and it is zero, so the expression is `0 || undefined` and the
		// limit becomes undefined. Every comparison against it is false.
		settings.Disabled = true
	}
	return settings, nil
}

// maximumDepthSettingsFrom recovers the settings from whatever the config layer handed over.
func maximumDepthSettingsFrom(options rule.Given[MaxDepthSettings]) MaxDepthSettings {
	if settings, ok := options.Read(); ok {
		return settings
	}
	return DefaultMaxDepthSettings()
}

var messageMaxDepthTooDeeply = rule.Message{
	Id: "tooDeeply",
	Description: "Each level of nesting is another condition the reader has to hold in their head " +
		"to know whether this line runs at all. Lift a branch into a named function, return early " +
		"to flatten the happy path, or invert a condition so the exceptional case exits first.",
}

// MaxDepth reports a block nested more deeply than the configured limit.
//
//	valid:   function f() { if (a) { if (b) {} } }         (limit 2)
//	valid:   function f() { if (a) {} else if (b) {} }     (an else-if is one chain, not two levels)
//	valid:   function f() { if (a) {} } function g() { if (b) {} }  (each function counts alone)
//	invalid: function f() { if (a) { if (b) { if (c) {} } } }       (limit 2)
//
// # The depth is per function, not per file, and that is the whole shape of the rule
//
// Upstream keeps a stack of counters, pushes one when it enters any function-like node, and counts
// blocks against the top of that stack. So two sibling functions each nesting three deep are two
// separate measurements rather than one of six, and a nested function resets the count rather than
// inheriting it. The corpus pins that: `function f() { if (a) { function g() { if (b) {} } } }`
// is clean at limit 1, because `g` starts over.
//
// # Enter and exit, with no exit hook, so the walk is explicit
//
// Upstream is a listener pair -- `IfStatement` and `IfStatement:exit` -- and this tree has neither
// an exit hook nor anything but a pre-order walk. So the whole rule runs inside one
// `KindSourceFile` listener that recurses itself, incrementing on the way down and decrementing on
// the way back up. That is the shape the standard names for a rule that has to know what it is
// inside, and it is why there are no per-kind listeners here.
//
// # `else if` is one construct wearing two nodes, and it is the rule's only real discrimination
//
// `if (a) {} else if (b) {}` parses as an IfStatement whose else-branch is another IfStatement, so
// a naive count reads a two-level nest where a reader sees one flat chain. Upstream's `isElseIf`
// tests `node.parent.type === "IfStatement" && node.parent.alternate === node`, and that maps here
// exactly: measured over the corpus shapes, an `else if` chain gives each inner IfStatement a
// parent IfStatement whose `ElseStatement` is that node, while `else { if (b) {} }` -- braces --
// gives the inner one a Block parent and correctly counts as a new level.
//
// # The span is the keyword, not the statement
//
// Upstream reports `loc: sourceCode.getFirstToken(node).loc`, so the finding covers `if`, `for`,
// `switch` and nothing else. Measured against the installed rule: columns 43-45 for an `if`,
// 27-33 for a `switch`. `rule.TokenRange` is the wrong helper here because it extends to
// `node.End()` and would underline the entire statement including its body;
// `scanner.GetRangeOfTokenAtPosition` alone is the token.
//
// # No fix
//
// Upstream offers none and none is possible: flattening a nest means extracting a function or
// inverting a condition, and only the author knows which.
var MaxDepth = rule.Rule[MaxDepthSettings]{
	Name: "max-depth",

	Run: func(ruleContext rule.Context, options rule.Given[MaxDepthSettings]) rule.Listeners {
		settings := maximumDepthSettingsFrom(options)
		if settings.Disabled {
			// `{"maximum": 0}` leaves upstream comparing against `undefined`, so it reports
			// nothing at all. Declining the file is the same verdict paid once rather than once
			// per block. See the decoder's option table for the measurement.
			return rule.Listeners{}
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				// The walk is done here rather than through per-kind listeners because the rule
				// needs a counter that rises entering a block and falls leaving it, and the
				// listener walk is pre-order with no exit hook to decrement on.
				walker := &maximumDepthWalker{ruleContext: ruleContext, settings: settings}
				// Upstream's `Program: startFunction`: the file itself is a function frame, so
				// top-level blocks are measured rather than ignored.
				walker.depths = append(walker.depths, 0)
				walker.walk(node)
			},
		}
	},
}

// maximumDepthIsElseIf is upstream's `isElseIf`: this IfStatement is the else-branch of another one, so
// it continues a chain rather than opening a new level.
func maximumDepthIsElseIf(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil || parent.Kind != ast.KindIfStatement {
		return false
	}
	statement := parent.AsIfStatement()
	return statement != nil && statement.ElseStatement == node
}
