// Package upstream adapts tsgolint's rules to verify's rule interface.
//
// tsgolint implements 40 type-aware typescript-eslint rules in Go against the same checker verify
// uses. Four are rules our gate enforces today, and they are the expensive kind to write correctly:
// they ask real questions of the type checker rather than matching a node shape.
//
// This file removes the per-rule interface work, which is the expensive half. It does not remove
// the per-rule sourcing work, and an earlier version of this comment claimed it did.
//
// What is actually proven: the adapter converts a vendored tsgolint rule into one verify can run,
// and `await_thenable` is the single rule vendored today. Reaching the other 39 means vendoring
// them from tsgolint's tree first. That is mechanical rather than a port, since no rule logic is
// edited, but it is not free, and quoting "forty rules for one unit of work" was a number nobody
// had paid. State the cost as: one function call per rule at the interface, plus sourcing.
//
// It is possible because verify's own rule interface was adapted from tsgolint's, and both compile
// against byte-identical shims over the same typescript-go commit. The two Rule types are the same
// shape over the same nominal types:
//
//	theirs: Rule{ Name string; Run func(RuleContext, any) RuleListeners }
//	ours:   Rule{ Name string; Run func(Context,     any) Listeners     }
//
// So the adaptation is a translation of the reporting surface, not of any rule logic. No upstream
// rule is edited, which is what keeps re-syncing with upstream a diff rather than a merge.
package upstream

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/verify/internal/rule"
	upstreamrule "github.com/system-inc/verify/internal/upstream/tsgolint/rule"
)

// exitListenerOffset is where tsgolint's pseudo-kinds begin.
//
// `ListenerOnExit(kind)` returns `kind + 1000`, with further offsets at 2000 and 4000 for the
// allow-pattern variants. A listener registered at one of those integers is not asking about a real
// node kind; it is asking to be called when the walk *leaves* a node.
//
// verify's walk has no exit visit. A listener registered at such an offset would be looked up
// against real node kinds, never match, and never fire — a rule that runs, reports nothing, and
// looks exactly like a rule that found nothing. That is the silent-death shape this project exists
// to prevent, so Adapt refuses rather than adapts. Five of the forty need it: no-misused-spread,
// no-unsafe-assignment, require-await, return-await, unbound-method.
const exitListenerOffset ast.Kind = 1000

// Adapt wraps an upstream rule so verify can run it, or returns an error saying why it cannot.
//
// It returns an error rather than a rule that quietly does less, because a linter's worst failure
// is a rule that is present and blind. Registration is the right place to find that out: it happens
// once at startup, where a failure is loud, rather than per file, where it is invisible.
func Adapt(subject upstreamrule.Rule) (rule.Rule, error) {
	if subject.Name == "" {
		return rule.Rule{}, fmt.Errorf("upstream rule has no name")
	}
	if subject.Run == nil {
		return rule.Rule{}, fmt.Errorf("upstream rule %q has no Run function", subject.Name)
	}

	adapted := rule.Rule{
		Name: subject.Name,

		// Every adapted rule is assumed to read the checker, because the adapter hands
		// context.TypeChecker to upstream below and cannot see whether the rule it wraps uses it.
		//
		// Declaring this per-rule from upstream metadata would be more precise and is not worth the
		// risk: the failure mode of under-declaring is a nil checker inside a type-aware rule, and
		// the failure mode of over-declaring is that a file pays for a lock it did not need. The
		// second is a measured cost, the first is a crash.
		NeedsTypeChecker: true,

		Run: func(context rule.Context, options any) rule.Listeners {
			upstreamListeners := subject.Run(upstreamContext(context, subject.Name), options)
			if len(upstreamListeners) == 0 {
				// The rule declined the file, which is the cheapest thing a rule can do and must
				// survive the adaptation unchanged.
				return nil
			}

			listeners := make(rule.Listeners, len(upstreamListeners))
			for kind, listener := range upstreamListeners {
				listeners[kind] = listener
			}
			return listeners
		},
	}

	return adapted, nil
}

// MustAdapt is Adapt for the registry, where a failure is a build-time mistake rather than a
// runtime condition.
//
// It panics, and that is deliberate: the registry is built at process start, so a rule that cannot
// be adapted must stop the tool immediately rather than be silently absent. Three configurations in
// the gate verify replaces ran successfully having loaded zero plugins, and this is the arrangement
// that makes that impossible here.
func MustAdapt(subject upstreamrule.Rule) rule.Rule {
	adapted, err := Adapt(subject)
	if err != nil {
		panic(fmt.Sprintf("adapting upstream rule: %v", err))
	}
	return adapted
}

// UsesExitListeners reports whether a rule registers any pseudo-kind listener.
//
// This has to be answered by running the rule, because the listener map is built per file rather
// than declared. Callers probe with a real parsed file; a rule that declines the probe file simply
// reports false, which is why this is a screening helper rather than a guarantee.
func UsesExitListeners(listeners upstreamrule.RuleListeners) bool {
	for kind := range listeners {
		if kind >= exitListenerOffset {
			return true
		}
	}
	return false
}

// upstreamContext builds the reporting surface an upstream rule expects.
//
// Every path funnels into verify's own Report, so an adapted rule's findings are indistinguishable
// from a native rule's downstream: they sort together, they filter through suppression identically,
// and they carry the same rule name. That last part matters more than it looks — a finding that
// reported under a different name would be unsuppressable by the comment its author wrote.
func upstreamContext(context rule.Context, ruleName string) upstreamrule.RuleContext {
	report := func(textRange core.TextRange, message upstreamrule.RuleMessage, fixes []upstreamrule.RuleFix, suggestions []upstreamrule.RuleSuggestion) {
		context.Report(rule.Diagnostic{
			RuleName:    ruleName,
			Range:       textRange,
			Message:     adaptMessage(message),
			SourceFile:  context.SourceFile,
			Fixes:       adaptFixes(fixes),
			Suggestions: adaptSuggestions(suggestions),
		})
	}

	return upstreamrule.RuleContext{
		SourceFile:  context.SourceFile,
		Program:     context.Program,
		TypeChecker: context.TypeChecker,

		ReportRange: func(textRange core.TextRange, message upstreamrule.RuleMessage) {
			report(textRange, message, nil, nil)
		},
		ReportRangeWithSuggestions: func(textRange core.TextRange, message upstreamrule.RuleMessage, suggestions ...upstreamrule.RuleSuggestion) {
			report(textRange, message, nil, suggestions)
		},
		ReportNode: func(node *ast.Node, message upstreamrule.RuleMessage) {
			report(node.Loc, message, nil, nil)
		},
		ReportNodeWithFixes: func(node *ast.Node, message upstreamrule.RuleMessage, fixes ...upstreamrule.RuleFix) {
			report(node.Loc, message, fixes, nil)
		},
		ReportNodeWithSuggestions: func(node *ast.Node, message upstreamrule.RuleMessage, suggestions ...upstreamrule.RuleSuggestion) {
			report(node.Loc, message, nil, suggestions)
		},
	}
}

func adaptMessage(message upstreamrule.RuleMessage) rule.Message {
	return rule.Message{Id: message.Id, Description: message.Description}
}

// adaptFixes carries an upstream fix slice of any length into one diagnostic, which makes this the
// live path to the edit engine's multi-fix hazard.
//
// The engine flattens a diagnostic's fixes into independent proposals, so two fixes reported
// together are judged separately and one may be refused while the other lands. For a pair that only
// means something jointly that is a half-application, and it can still parse. See ProposalsFrom in
// internal/fix for the full statement.
//
// The reason it is worth saying here rather than only there: an adapted rule reaches that path
// without anyone writing one of our rules. No vendored rule emits two fixes in one report today,
// and the first one that does arrives through this function.
func adaptFixes(fixes []upstreamrule.RuleFix) []rule.Fix {
	if len(fixes) == 0 {
		return nil
	}
	adapted := make([]rule.Fix, 0, len(fixes))
	for _, fix := range fixes {
		adapted = append(adapted, rule.Fix{Range: fix.Range, Text: fix.Text})
	}
	return adapted
}

// adaptSuggestions preserves the fix/suggestion distinction rather than flattening it.
//
// The difference is intent, not confidence: a fix preserves what the code means and may be applied
// unattended, while a suggestion changes it and needs a human to agree. Collapsing the two would
// hand the edit engine permission it was never given.
func adaptSuggestions(suggestions []upstreamrule.RuleSuggestion) []rule.Suggestion {
	if len(suggestions) == 0 {
		return nil
	}
	adapted := make([]rule.Suggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		adapted = append(adapted, rule.Suggestion{
			Message: adaptMessage(suggestion.Message),
			Fixes:   adaptFixes(suggestion.Fixes()),
		})
	}
	return adapted
}
