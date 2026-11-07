package tailwind

import (
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
)

func messageUnknownClass(className string) rule.Message {
	return rule.Message{
		Id: "unknownClass",
		Description: "The class \"" + className + "\" is not one Tailwind knows, so it generates no CSS " +
			"and does nothing. Nothing errors: the class sits in the markup, the element renders, and " +
			"the style it was meant to apply is simply absent. That is usually a typo, and occasionally " +
			"a class from a stylesheet the design system does not know about, which needs adding to the " +
			"ignore list rather than fixing.",
	}
}

// NoUnknownClassesOptions lets a project name the surfaces that carry class strings, and exempt
// classes that are real but come from somewhere Tailwind cannot see.
type NoUnknownClassesOptions struct {
	TailwindLocationOptions
	TailwindClassLiteralOptions
	// Ignore lists regular expressions for classes to leave alone. Required in practice rather than
	// optional: a project with any hand-written CSS has class names Tailwind does not define, and
	// without exempting them the rule reports correct code.
	Ignore []string `json:"ignore"`
}

// NoUnknownClasses reports class names Tailwind does not define.
//
//	valid:   <div className="flex items-center" />
//	valid:   <div className="from-black/70 to-transparent" />
//	valid:   <div className="group peer" />
//	invalid: <div className="flx items-center" />
//	invalid: <div className="text-huge" />
//
// The failure this catches is silent by construction. An unknown class generates no CSS, so nothing
// errors, the element renders, and the style it was meant to apply is simply absent. There is no
// compiler for class names, which is why lint is the only layer that can see this at all.
//
// # Existence is a different question from what a class declares
//
// Every other rule in this package asks what a class sets. This one asks whether it exists, and
// answering it from the property tables is wrong in the direction that reports correct code.
// `from-black/70` sets only `--tw-gradient-from`, `container` emits several rules, and `fade-in`
// sets only `--enter-opacity`; all three are deliberately absent from those tables and all three are
// real. Measured on the ahra tree, that mistake produced 12 findings on valid classes.
//
// So existence is asked of the design system rather than of a table. See class_existence.go: the
// question is whether the repository in front of the linter can read the class as a candidate, which
// is the only form of the question that answers correctly for a repository this port has never seen.
// Absence from a properties table is not evidence a class does not exist, and presence in a
// generated one is not evidence that it does.
//
// # What it deliberately does not check
//
// A known root with an unknown value is not reported. `w-notavalue` has root `w` and does not
// compile, and catching it would need the theme's per-root value scales plus the arbitrary-value
// grammar, which is closer to reimplementing the utility resolver than to reading a table.
//
// That is a real gap and it is the safe direction: this rule catches every typo'd utility name and
// every stray class, and misses a typo'd value on a real root. Under-reporting leaves a defect for
// someone to find later; over-reporting on correct code is how a rule gets turned off, and this rule
// in particular gets turned off the first time it flags a working class.
var NoUnknownClasses = rule.Rule[NoUnknownClassesOptions]{
	Name: "better-tailwindcss/no-unknown-classes",
	// The design system: its stylesheets, read through the recording file system (DesignSystemFileSystem).
	ProgramReads: rule.ReadsCompilerOptions | rule.ReadsDesignSystem,
	Run: func(ruleContext rule.Context, options rule.Given[NoUnknownClassesOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}

		// Read before the design system is resolved: reading is pure, and a run that declines for want
		// of a Tailwind entry point must still reach it, or the registry's decoder agreement check
		// never looks at this rule (#zwd43jn).
		configured, isConfigured := options.Read()

		// Resolved once per file and before the listeners are built, matching
		// `enforce-consistent-class-order`. The two failure states are told apart rather than
		// collapsed: a project with no Tailwind is silence with nothing wrong, and a project whose
		// CSS will not parse is reported once per file. This rule needs that distinction more than
		// the others do, because a design system that failed to load would otherwise make every
		// class in the tree read as unknown.
		designSystem := DesignSystemForProgramAt(ruleContext.Program, configured.Location())
		if designSystem.Error != nil {
			if ruleContext.Program == nil {
				return nil
			}
			// No Tailwind entry point is silence with nothing wrong in a project without Tailwind, and
			// a skip --coverage names, so a project that enables this rule without one can see it (#pa7k7zv).
			if skipWithoutTailwind(ruleContext, designSystem.Error) {
				return nil
			}
			return declineListeners(ruleContext, "no-unknown-classes", designSystem)
		}

		surfaces := DefaultClassLiteralSurfaces()
		var ignore []string
		if isConfigured {
			surfaces = configured.ClassLiteralSurfaces()
			ignore = configured.Ignore
		}

		reader := surfaces.ReaderFor(ruleContext.FileCache)
		ignored := compileIgnorePatterns(ignore)

		report := func(node *ast.Node) {
			for _, literal := range reader.ClassLiteralsIn(node) {
				classes := SplitClasses(literal.Text)
				for index, className := range classes {
					if isConcatenatedEdgeClass(literal, index, len(classes)) {
						continue
					}
					if isIgnored(className, ignored) || classExistsIn(className, designSystem.System) {
						continue
					}
					ruleContext.ReportRange(literal.Range, messageUnknownClass(className))
				}
			}
			// The static runs of a template with holes, each class judged as upstream judges it, except
			// a class a hole touches: `notaclass${x}` is half of a class the hole completes, and upstream
			// skips it for the reason isConcatenatedEdgeClass gives.
			sourceText := ruleContext.SourceFile.Text()
			for _, segments := range reader.ClassTemplateSegmentsIn(node) {
				for _, class := range TemplateClassesOf(sourceText, segments) {
					if class.GluedLeading || class.GluedTrailing {
						continue
					}
					if isIgnored(class.Text, ignored) || classExistsIn(class.Text, designSystem.System) {
						continue
					}
					ruleContext.ReportRange(class.Range, messageUnknownClass(class.Text))
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

// isConcatenatedEdgeClass says a literal's first or last class is joined to another value with no
// whitespace between, so it is only part of a class: `'text-' + size` holds `text-`, which the value
// completes.
//
// Upstream 4.9.1's no-unknown-classes skips exactly these, through isConcatenatedLeft and
// isConcatenatedRight, measured under ESLint with ahra's theme: `'notaclass' + x` and
// `x + 'notaclass'` are silent, `'notaclass ' + x` reports. Reporting them would call a class
// unknown because the linter could not see the rest of it. The literal's Edges are those two
// questions already, since they say which end touches class text once the value is joined.
//
// Whitespace here is JavaScript's, decoded a rune at a time, because that is what SplitClasses splits
// on and what upstream's `\s` means: `'notaclass\u00a0' + x` and `x + '\u3000notaclass'` report on
// 4.9.1. Reading one byte against an ASCII set took the no-break space's last byte for class text and
// skipped a class that the split had already set apart.
func isConcatenatedEdgeClass(literal ClassLiteral, index int, count int) bool {
	if literal.Text == "" {
		return false
	}
	if index == 0 && literal.Edges.Leading {
		first, _ := utf8.DecodeRuneInString(literal.Text)
		if !text.IsWhitespace(first) {
			return true
		}
	}
	if index == count-1 && literal.Edges.Trailing {
		last, _ := utf8.DecodeLastRuneInString(literal.Text)
		if !text.IsWhitespace(last) {
			return true
		}
	}
	return false
}
