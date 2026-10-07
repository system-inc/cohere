package tailwind

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// messageConcatenatedClass names the fragment that got broken.
//
// The generic form of this message ("avoid dynamic class construction") tells a reader the shape of
// the problem and not where it is, which in a forty-class template is the difference between a fix
// and a shrug.
func messageConcatenatedClass(fragment string) rule.Message {
	return rule.Message{
		Id: "concatenatedClass",
		Description: "The fragment \"" + fragment + "\" is joined to a runtime value with no " +
			"space between them, so the class name it forms never appears literally in the source. " +
			"Tailwind finds classes by scanning text, so it will not generate this one and the style " +
			"silently will not apply. Write the whole class names out and switch between them, rather " +
			"than assembling one from pieces.",
	}
}

// NoConcatenatedClassesOptions lets a project name the surfaces that carry class strings.
type NoConcatenatedClassesOptions struct {
	TailwindLocationOptions
	TailwindClassLiteralOptions
}

// NoConcatenatedClasses reports a class fragment glued to an interpolated value.
//
//	valid:   <div className={`flex ${extra}`} />
//	valid:   <div className={`${extra} flex`} />
//	valid:   <div className={isWide ? 'px-8' : 'px-4'} />
//	invalid: <div className={`px-${size}`} />
//	invalid: <div className={`${prefix}-4`} />
//
// Tailwind finds the classes it must generate by scanning source text for things that look like
// class names. A class assembled at runtime never appears as text, so it is never generated, and the
// failure is entirely silent: the markup is correct, the class is on the element, and the rule that
// would style it was never written into the stylesheet. Nothing errors and the layout is simply
// wrong on one breakpoint until somebody notices.
//
// The name understates what the rule checks. It is not "was this string built dynamically" but "does
// a class fragment touch an interpolation with no whitespace at the seam", and the difference is the
// whole rule: `flex ${extra}` is fine because `flex` is a complete class name that Tailwind can see,
// while `px-${size}` is not because `px-` is not a class name at all.
//
// Measured against the upstream plugin rather than inferred from it. Reporting every interpolated
// class literal would flag 141 literals in the ahra tree that are all fine; upstream reports a seam,
// not a template.
//
// A `+` is a seam too, `'px-' + size`, when the string is read at all (isConcatenatedLiteral). Only a
// selector with matchers reaches a `+` operand: one with none reads a direct string and nothing
// nested, so under ahra's legacy settings upstream was silent on `'px-' + size` and on
// `'bg-[' + color + ']'`, and with its defaults it reports both. An earlier version of this comment
// read that silence as upstream's rule, measured under those settings (#btxd64n).
//
// No fix, deliberately. The repair is to write out the class names the code is choosing between,
// which means knowing which ones those are. A rule cannot know that, and a fix that guessed would
// produce a class list the author never intended.
var NoConcatenatedClasses = rule.Rule{
	Name: "better-tailwindcss/no-concatenated-classes",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		surfaces := DefaultClassLiteralSurfaces()
		if configured, isConfigured := rule.OptionsAs[NoConcatenatedClassesOptions](options); isConfigured {
			surfaces = configured.ClassLiteralSurfaces()
		}

		reader := surfaces.ReaderFor(ctx.FileCache)

		report := func(node *ast.Node) {
			for _, template := range reader.ClassTemplatesIn(node) {
				for _, boundary := range template.Boundaries {
					ctx.ReportRange(boundary.Range, messageConcatenatedClass(boundary.Fragment))
				}
			}
		}

		listeners := rule.Listeners{}
		for _, kind := range ListenerKinds() {
			listeners[kind] = report
		}
		return listeners
	},
}
