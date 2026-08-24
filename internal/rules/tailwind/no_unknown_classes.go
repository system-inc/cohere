package tailwind

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	tailwindengine "github.com/system-inc/verify/internal/tailwind"
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
	Attributes []string
	Callees    []string
	Variables  []string
	// Ignore lists regular expressions for classes to leave alone. Required in practice rather than
	// optional: a project with any hand-written CSS has class names Tailwind does not define, and
	// without exempting them the rule reports correct code.
	Ignore []string
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
// So existence reads its own tables, generated as the union of every functional root and every
// static name the design system defines. Absence from a properties table is not evidence a class
// does not exist.
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
var NoUnknownClasses = rule.Rule{
	Name: "no-unknown-classes",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		settings := DefaultClassLiteralSettings()
		var ignore []string
		if configured, isConfigured := options.(NoUnknownClassesOptions); isConfigured {
			if len(configured.Attributes) > 0 {
				settings.AttributeNames = configured.Attributes
			}
			if len(configured.Callees) > 0 {
				settings.CalleeNames = configured.Callees
			}
			if len(configured.Variables) > 0 {
				settings.VariablePatterns = configured.Variables
			}
			ignore = configured.Ignore
		}

		reader := NewClassLiteralReader(settings)
		ignored := compileIgnorePatterns(ignore)

		report := func(node *ast.Node) {
			for _, literal := range reader.ClassLiteralsIn(node) {
				for _, className := range SplitClasses(literal.Text) {
					if isIgnored(className, ignored) || classExists(className) {
						continue
					}
					ctx.ReportRange(literal.Range, messageUnknownClass(className))
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

// alwaysKnownClasses are markers Tailwind treats specially rather than as utilities.
//
// `group` and `peer` generate no CSS of their own: they exist to be referenced by `group-hover:` and
// `peer-checked:` variants on other elements. Upstream ignores them by explicit regular expression
// for exactly this reason, and a port that omitted them would report two of the most common classes
// in any real codebase.
var alwaysKnownClasses = regexp.MustCompile(`^(group|peer)(/\S*)?$`)

// arbitraryPropertyPattern matches a whole-property escape hatch such as `[font:inherit]`.
//
// These have no root to look up: the author wrote the CSS declaration directly. Tailwind accepts any
// syntactically valid one, so there is nothing to check against and reporting them would flag a
// deliberate feature.
var arbitraryPropertyPattern = regexp.MustCompile(`^\[[^\]]+:[^\]]*\]$`)

// classExists reports whether Tailwind defines a class, by name rather than by what it declares.
func classExists(className string) bool {
	// Checked before variants are stripped: `dissectClass` splits on the last colon, and an
	// arbitrary property carries one inside its own brackets, so `[font:inherit]` would be cut into
	// a variant `[font:` and a base `inherit]`. The brackets are the class, not a prefix.
	if arbitraryPropertyPattern.MatchString(className) {
		return true
	}

	_, base, _ := dissectClass(className)
	if base == "" {
		return true
	}

	if alwaysKnownClasses.MatchString(base) {
		return true
	}
	if arbitraryPropertyPattern.MatchString(base) {
		return true
	}
	if tailwindengine.KnownStatics[base] {
		return true
	}

	// A functional class is its root plus a value. The root must end at a value boundary, so `p`
	// does not claim `px-4`: taking a shorter match would make every misspelling that happens to
	// start with a real root look valid.
	for root := range tailwindengine.KnownRoots {
		if !strings.HasPrefix(base, root) {
			continue
		}
		if len(base) == len(root) {
			return true
		}
		remainder := base[len(root):]
		// `from-black/70` is root `from` with value `black/70`; `w-[13px]` is root `w` with an
		// arbitrary value; `bg-(--custom)` is root `bg` with a custom-property value.
		if remainder[0] == '-' || remainder[0] == '/' || remainder[0] == '[' || remainder[0] == '(' {
			return true
		}
	}

	return false
}
