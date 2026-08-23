package nexus

import (
	"regexp"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// alwaysAllowedSingleLetters are the coordinate and math names, where the single letter is the
// conventional spelling and a longer one would read worse.
var alwaysAllowedSingleLetters = map[string]bool{"x": true, "y": true, "z": true}

var (
	handlerNamePattern     = regexp.MustCompile(`(?i)handle|on[A-Z]|event`)
	eventHandlerKeyPattern = regexp.MustCompile(`^on[A-Z]`)
	singleLowercaseLetter  = regexp.MustCompile(`^[a-z]$`)
)

func messageNoAmbiguousE(contextHint string, suggestedName string) rule.Message {
	return rule.Message{
		Id: "noAmbiguousE",
		Description: `Variable named "e" is too ambiguous` + contextHint + `. It is the one name that ` +
			`could be an error or an event, and a reader has to find the declaration to learn which. Use "` +
			suggestedName + `" or a more descriptive name.`,
	}
}

func messageNoSingleLetter(name string) rule.Message {
	return rule.Message{
		Id: "noSingleLetter",
		Description: `Single-letter identifier "` + name + `" is not descriptive enough. The name is read ` +
			`everywhere it is used and declared only once, so the saving is at the declaration and the cost ` +
			`is at every call site.`,
	}
}

var messageNoUnderscore = rule.Message{
	Id: "noUnderscore",
	Description: `Identifier "_" is not descriptive enough. Name it explicitly, or prefix an underscore ` +
		`to a real name such as "_event" when the point is that the value is deliberately unused.`,
}

// ConsistencyNoAmbiguousIdentifier bans single-letter identifiers and the bare underscore.
//
//	valid:   catch (error) { }
//	valid:   const x = 1
//	valid:   items.sort((a, b) => a - b)
//	invalid: catch (e) { }
//	invalid: const n = 1
//	invalid: const _ = 1
//
// The exemptions carry the judgment. Coordinates keep x, y, and z, where the single letter is the
// conventional spelling. A sort comparator keeps a and b, because that is the shape everyone reads
// and naming them would be noise. Property names, object keys, import specifiers, and type property
// signatures are skipped entirely: those are not bindings the reader has to track, they are names
// somebody else chose.
//
// `e` gets its own message because it is the ambiguous one: it could be an error or an event, and
// which one it is decides the right name. The rule infers from context and says what it inferred.
//
// No fix, and that is a departure from the TypeScript original, which rewrites `e` to `event` or
// `error` in place. Renaming a binding without following its references through scope leaves every
// other use of `e` pointing at a name that no longer exists, so the fix would turn a style finding
// into a broken file. It is the same trap import-require-node-namespace declines for the same
// reason. The suggested name travels in the message instead, where a reader applies it with the
// scope in front of them.
var ConsistencyNoAmbiguousIdentifier = rule.Rule{
	Name: "consistency-no-ambiguous-identifier",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindIdentifier: func(node *ast.Node) {
				name := node.Text()

				// Only single lowercase letters and the bare underscore are in scope. An uppercase
				// single letter is a type parameter (T, K, V, R, E) and is left alone.
				if name != "_" && !singleLowercaseLetter.MatchString(name) {
					return
				}

				// These are names somebody else chose, not bindings this file has to live with.
				if isNonBindingIdentifier(node) {
					return
				}

				if name == "_" {
					ctx.ReportNode(node, messageNoUnderscore)
					return
				}
				if alwaysAllowedSingleLetters[name] {
					return
				}
				if name == "e" {
					suggestedName, contextHint := inferEventOrErrorContext(node)
					ctx.ReportNode(node, messageNoAmbiguousE(contextHint, suggestedName))
					return
				}
				// A sort comparator is the one place a and b read correctly.
				if (name == "a" || name == "b") && isInsideSortComparator(node) {
					return
				}

				ctx.ReportNode(node, messageNoSingleLetter(name))
			},
		}
	},
}

// isNonBindingIdentifier reports whether an identifier names a property, key, import binding, or
// type member rather than a variable the reader has to track.
func isNonBindingIdentifier(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}

	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		// The property half of `thing.e`, but not the object half.
		access := parent.AsPropertyAccessExpression()
		return access != nil && access.Name() == node

	case ast.KindPropertyAssignment:
		// The key half of `{ e: 1 }`, but not a shorthand value.
		assignment := parent.AsPropertyAssignment()
		return assignment != nil && assignment.Name() == node

	case ast.KindImportSpecifier, ast.KindExportSpecifier:
		return true

	case ast.KindPropertySignature:
		signature := parent.AsPropertySignatureDeclaration()
		return signature != nil && signature.Name() == node

	case ast.KindJsxOpeningElement, ast.KindJsxClosingElement, ast.KindJsxSelfClosingElement,
		ast.KindJsxAttribute:
		// A JSX tag and a JSX attribute are identifiers syntactically and neither is a binding. An
		// intrinsic element is named by HTML, not by us, so `<p>` and `<b>` are not names anyone
		// chose or has to hold in their head.
		//
		// This cost 3,081 false findings on the ahra tree before it was caught. The port visited
		// every KindIdentifier and reproduced only the exemptions the TypeScript original spells
		// out; the original never needed a JSX case because its parser gives a JSX name a distinct
		// node type. That is the general shape of this class of divergence: an exemption the
		// original gets from its AST for free has to be written down in ours.
		return true
	}
	return false
}

// isInsideSortComparator reports whether an identifier sits in a function passed as the first
// argument to a `.sort(...)` call.
func isInsideSortComparator(node *ast.Node) bool {
	for current := node.Parent; current != nil; current = current.Parent {
		if current.Kind != ast.KindArrowFunction && current.Kind != ast.KindFunctionExpression {
			continue
		}

		call := current.Parent
		if call == nil || call.Kind != ast.KindCallExpression {
			return false
		}
		callExpression := call.AsCallExpression()
		if callExpression == nil || callExpression.Arguments == nil ||
			len(callExpression.Arguments.Nodes) == 0 || callExpression.Arguments.Nodes[0] != current {
			return false
		}

		callee := callExpression.Expression
		if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
			return false
		}
		access := callee.AsPropertyAccessExpression()
		if access == nil {
			return false
		}
		calleeName := access.Name()
		return calleeName != nil && calleeName.Text() == "sort"
	}
	return false
}

// inferEventOrErrorContext guesses whether an `e` is an error or an event, and says which it
// guessed so a reader can disagree with the reasoning rather than just the verdict.
func inferEventOrErrorContext(node *ast.Node) (string, string) {
	// A catch parameter is unambiguous. The identifier's parent is the VariableDeclaration that
	// binds it, and the CatchClause is one level above that, so this reaches through both rather
	// than testing the immediate parent.
	if declaration := node.Parent; declaration != nil && declaration.Kind == ast.KindVariableDeclaration {
		if clause := declaration.Parent; clause != nil && clause.Kind == ast.KindCatchClause {
			if declaration.Name() == node {
				return "error", " (appears to be an error)"
			}
		}
	}

	for current := node.Parent; current != nil; current = current.Parent {
		parent := current.Parent
		if parent == nil {
			break
		}

		switch parent.Kind {
		case ast.KindPropertyAssignment:
			// A handler passed as `{ onClick: (e) => ... }`.
			assignment := parent.AsPropertyAssignment()
			if assignment != nil && assignment.Initializer == current {
				if key := assignment.Name(); key != nil && eventHandlerKeyPattern.MatchString(key.Text()) {
					return "event", " (appears to be an event)"
				}
			}

		case ast.KindJsxExpression:
			// A handler passed as `onClick={(e) => ...}`.
			if attribute := parent.Parent; attribute != nil && attribute.Kind == ast.KindJsxAttribute {
				jsxAttribute := attribute.AsJsxAttribute()
				if jsxAttribute != nil {
					if name := jsxAttribute.Name(); name != nil && eventHandlerKeyPattern.MatchString(name.Text()) {
						return "event", " (appears to be an event)"
					}
				}
			}

		case ast.KindVariableDeclaration:
			// Assigned to something named like a handler.
			declaration := parent.AsVariableDeclaration()
			if declaration != nil && declaration.Initializer == current {
				if name := declaration.Name(); name != nil && name.Kind == ast.KindIdentifier &&
					handlerNamePattern.MatchString(name.Text()) {
					return "event", " (appears to be an event)"
				}
			}
		}

		// A function declaration named like a handler.
		if current.Kind == ast.KindFunctionDeclaration {
			declaration := current.AsFunctionDeclaration()
			if declaration != nil {
				if name := declaration.Name(); name != nil && handlerNamePattern.MatchString(name.Text()) {
					return "event", " (appears to be an event)"
				}
			}
		}
	}

	return "event", " (context unclear)"
}
