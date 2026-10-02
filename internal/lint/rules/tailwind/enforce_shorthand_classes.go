package tailwind

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	tailwindengine "github.com/system-inc/cohere/internal/lint/rules/tailwind/collapse"
)

// messageShorthandClasses names the longhands and the class that replaces them.
func messageShorthandClasses(longhands string, shorthands string) rule.Message {
	return rule.Message{
		Id: "shorthandClasses",
		Description: "\"" + longhands + "\" is the longhand spelling of \"" + shorthands + "\", " +
			"which sets the same declarations in one class. The pair carries no information the " +
			"single class does not, and it costs the next reader a moment working out whether the " +
			"two halves were meant to move together or drifted apart. Write it as \"" +
			shorthands + "\".",
	}
}

// EnforceShorthandClassesOptions lets a project name the surfaces that carry class strings.
type EnforceShorthandClassesOptions struct {
	Attributes []string
	Callees    []string
	Variables  []string
}

// shorthandRule is one collapse: a set of longhand patterns and the classes they become.
type shorthandRule struct {
	Longhands  []string
	Shorthands []string
}

// shorthandGroups is upstream's table, transcribed rather than reasoned about.
//
// Extracted mechanically from `enforce-shorthand-classes.ts` at 4.7.0 rather than retyped, because
// it is 48 rules and a transcription error would be a silent miss on one utility family rather than
// a failure anywhere.
//
// The two levels are load-bearing. The outer level groups rules that describe the same property
// family, and only one rule per family may fire, so `px-4 py-4 p-4` does not report twice. Within a
// family the rules are tried longest-first, which is what makes `ml-1 mr-1 mt-1 mb-1` collapse
// straight to `m-1` rather than to `mx-1 my-1` and then again.
var shorthandGroups = [][]shorthandRule{
	{
		{Longhands: []string{"w-(.*)", "h-(.*)"}, Shorthands: []string{"size-$1"}},
	},
	{
		{Longhands: []string{"ml-(.*)", "mr-(.*)", "mt-(.*)", "mb-(.*)"}, Shorthands: []string{"m-$1"}},
		{Longhands: []string{"mx-(.*)", "my-(.*)"}, Shorthands: []string{"m-$1"}},
		{Longhands: []string{"ms-(.*)", "me-(.*)"}, Shorthands: []string{"mx-$1"}},
		{Longhands: []string{"ml-(.*)", "mr-(.*)"}, Shorthands: []string{"mx-$1"}},
		{Longhands: []string{"mt-(.*)", "mb-(.*)"}, Shorthands: []string{"my-$1"}},
	},
	{
		{Longhands: []string{"pl-(.*)", "pr-(.*)", "pt-(.*)", "pb-(.*)"}, Shorthands: []string{"p-$1"}},
		{Longhands: []string{"px-(.*)", "py-(.*)"}, Shorthands: []string{"p-$1"}},
		{Longhands: []string{"ps-(.*)", "pe-(.*)"}, Shorthands: []string{"px-$1"}},
		{Longhands: []string{"pl-(.*)", "pr-(.*)"}, Shorthands: []string{"px-$1"}},
		{Longhands: []string{"pt-(.*)", "pb-(.*)"}, Shorthands: []string{"py-$1"}},
	},
	{
		{Longhands: []string{"border-t-(.*)", "border-b-(.*)", "border-l-(.*)", "border-r-(.*)"}, Shorthands: []string{"border-$1"}},
		{Longhands: []string{"border-x-(.*)", "border-y-(.*)"}, Shorthands: []string{"border-$1"}},
		{Longhands: []string{"border-s-(.*)", "border-e-(.*)"}, Shorthands: []string{"border-x-$1"}},
		{Longhands: []string{"border-l-(.*)", "border-r-(.*)"}, Shorthands: []string{"border-x-$1"}},
		{Longhands: []string{"border-t-(.*)", "border-b-(.*)"}, Shorthands: []string{"border-y-$1"}},
	},
	{
		{Longhands: []string{"border-spacing-x-(.*)", "border-spacing-y-(.*)"}, Shorthands: []string{"border-spacing-$1"}},
	},
	{
		{Longhands: []string{"rounded-tl-(.*)", "rounded-tr-(.*)", "rounded-bl-(.*)", "rounded-br-(.*)"}, Shorthands: []string{"rounded-$1"}},
		{Longhands: []string{"rounded-t-(.*)", "rounded-b-(.*)"}, Shorthands: []string{"rounded-$1"}},
		{Longhands: []string{"rounded-l-(.*)", "rounded-r-(.*)"}, Shorthands: []string{"rounded-$1"}},
		{Longhands: []string{"rounded-tl-(.*)", "rounded-tr-(.*)"}, Shorthands: []string{"rounded-t-$1"}},
		{Longhands: []string{"rounded-bl-(.*)", "rounded-br-(.*)"}, Shorthands: []string{"rounded-b-$1"}},
		{Longhands: []string{"rounded-tl-(.*)", "rounded-bl-(.*)"}, Shorthands: []string{"rounded-l-$1"}},
		{Longhands: []string{"rounded-tr-(.*)", "rounded-br-(.*)"}, Shorthands: []string{"rounded-r-$1"}},
	},
	{
		{Longhands: []string{"scroll-mt-(.*)", "scroll-mb-(.*)", "scroll-ml-(.*)", "scroll-mr-(.*)"}, Shorthands: []string{"scroll-m-$1"}},
		{Longhands: []string{"scroll-mx-(.*)", "scroll-my-(.*)"}, Shorthands: []string{"scroll-m-$1"}},
		{Longhands: []string{"scroll-ms-(.*)", "scroll-me-(.*)"}, Shorthands: []string{"scroll-mx-$1"}},
		{Longhands: []string{"scroll-ml-(.*)", "scroll-mr-(.*)"}, Shorthands: []string{"scroll-mx-$1"}},
		{Longhands: []string{"scroll-mt-(.*)", "scroll-mb-(.*)"}, Shorthands: []string{"scroll-my-$1"}},
	},
	{
		{Longhands: []string{"scroll-pt-(.*)", "scroll-pb-(.*)", "scroll-pl-(.*)", "scroll-pr-(.*)"}, Shorthands: []string{"scroll-p-$1"}},
		{Longhands: []string{"scroll-px-(.*)", "scroll-py-(.*)"}, Shorthands: []string{"scroll-p-$1"}},
		{Longhands: []string{"scroll-pl-(.*)", "scroll-pr-(.*)"}, Shorthands: []string{"scroll-px-$1"}},
		{Longhands: []string{"scroll-ps-(.*)", "scroll-pe-(.*)"}, Shorthands: []string{"scroll-px-$1"}},
		{Longhands: []string{"scroll-pt-(.*)", "scroll-pb-(.*)"}, Shorthands: []string{"scroll-py-$1"}},
	},
	{
		{Longhands: []string{"top-(.*)", "right-(.*)", "bottom-(.*)", "left-(.*)"}, Shorthands: []string{"inset-$1"}},
		{Longhands: []string{"inset-x-(.*)", "inset-y-(.*)"}, Shorthands: []string{"inset-$1"}},
	},
	{
		{Longhands: []string{"divide-x-(.*)", "divide-y-(.*)"}, Shorthands: []string{"divide-$1"}},
	},
	{
		{Longhands: []string{"space-x-(.*)", "space-y-(.*)"}, Shorthands: []string{"space-$1"}},
	},
	{
		{Longhands: []string{"gap-x-(.*)", "gap-y-(.*)"}, Shorthands: []string{"gap-$1"}},
	},
	{
		{Longhands: []string{"translate-x-(.*)", "translate-y-(.*)"}, Shorthands: []string{"translate-$1"}},
	},
	{
		{Longhands: []string{"rotate-x-(.*)", "rotate-y-(.*)"}, Shorthands: []string{"rotate-$1"}},
	},
	{
		{Longhands: []string{"skew-x-(.*)", "skew-y-(.*)"}, Shorthands: []string{"skew-$1"}},
	},
	{
		{Longhands: []string{"scale-x-(.*)", "scale-y-(.*)", "scale-z-(.*)"}, Shorthands: []string{"scale-$1", "scale-3d"}},
		{Longhands: []string{"scale-x-(.*)", "scale-y-(.*)"}, Shorthands: []string{"scale-$1"}},
	},
	{
		{Longhands: []string{"content-(.*)", "justify-content-(.*)"}, Shorthands: []string{"place-content-$1"}},
		{Longhands: []string{"items-(.*)", "justify-items-(.*)"}, Shorthands: []string{"place-items-$1"}},
		{Longhands: []string{"self-(.*)", "justify-self-(.*)"}, Shorthands: []string{"place-self-$1"}},
	},
	{
		{Longhands: []string{"overflow-hidden", "text-ellipsis", "whitespace-nowrap"}, Shorthands: []string{"truncate"}},
	},
}

// EnforceShorthandClasses reports longhand class pairs that a single class already expresses.
//
//	valid:   <div className="px-4" />
//	invalid: <div className="ps-4 pe-4" />
//	invalid: <div className="ml-1 mr-1 mt-1 mb-1" />
//
// # What this is for
//
// `ps-4 pe-4` and `px-4` emit the same declarations, so nothing surfaces the difference. The cost
// is that the pair is ambiguous evidence: it reads either as a deliberate choice to set the two
// sides independently, or as two edits that met in the middle, and the next person to change one
// side cannot tell which. The single class says the two move together.
//
// # Everything about a group has to agree, not just the value
//
// The value is the obvious half and the rest is what a reader would miss. Upstream requires the
// captured values to match (`ps-4 pe-8` is not a pair), and also the variants (`ps-4 hover:pe-4` is
// not), the negative sign (`-mt-2 mb-2` is not), and the important marker (`!pt-2 pb-2` is not).
// Each of those was measured against upstream rather than assumed, and each one silently widens the
// rule if dropped.
//
// The rebuilt class carries the variants, the sign and the marker of the group it replaces, so
// `hover:ps-4 hover:pe-4` becomes `hover:px-4` and `-mt-2 -mb-2` becomes `-my-2`.
//
// # Membership is checked, as upstream checks it
//
// Upstream asks the design system whether the shorthand it is about to suggest exists, and declines
// when it does not. This rule skipped that on the reasoning that a missing shorthand needs a theme
// that removed a framework utility, a shape neither corpus repository has, and that
// `no-unknown-classes` would catch the suggestion if one ever appeared.
//
// The table is patterns, and a pattern can build a class Tailwind never had. `w-screen h-screen`
// matches `w-(.*) h-(.*)` and suggests `size-screen`, which does not exist: `w-screen` is `100vw`
// and `h-screen` is `100vh`, so no one class sets both. Found when nested literals reached two of
// them in ahra (#1hmzh0z), and the author following the finding would have deleted a working pair
// for a class that does nothing. `no-unknown-classes` did catch it, one edit too late.
//
// So the design system is asked when there is one. Without a program (a syntactic fixture) or
// without a Tailwind entry point there is nothing to ask, and the rule reports as it did.
//
// Not fixable, for the reason its siblings are not: a class literal here wraps across lines with
// indentation that carries intent, and a fixer would be the first thing to reflow it.
//
// # Declining the fixer removes a finding, not just a fix
//
// Upstream reports every collapse twice on a real tree: once as the longhand pair, and once as
// "Unnecessary whitespace" at the gap its own fixer leaves when it replaces the second class with an
// empty string. Measured on Kirk's tree, where its four findings are the same two sites. This rule
// reports two, and the difference is entirely that second class of finding, which describes the
// state upstream's fix passes through rather than anything in the source a reader wrote.
var EnforceShorthandClasses = rule.Rule{
	Name: "better-tailwindcss/enforce-shorthand-classes",
	// The design system, whose stylesheets are read off the file system, outside the program.
	ProgramReads: rule.ReadsCompilerOptions | rule.ReadsOtherFiles,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		// nil when there is no design system to ask, and classExistsIn answers true for nil.
		var system *tailwindengine.LoadedDesignSystem
		if ctx.Program != nil {
			if designSystem := DesignSystemForProgram(ctx.Program); designSystem.Err == nil {
				system = designSystem.System
			}
		}

		settings := DefaultClassLiteralSettings()
		if configured, isConfigured := rule.OptionsAs[EnforceShorthandClassesOptions](options); isConfigured {
			if len(configured.Attributes) > 0 {
				settings.AttributeNames = configured.Attributes
			}
			if len(configured.Callees) > 0 {
				settings.CalleeNames = configured.Callees
			}
			if len(configured.Variables) > 0 {
				settings.VariablePatterns = configured.Variables
			}
		}

		reader := NewClassLiteralReader(settings)

		report := func(node *ast.Node) {
			for _, literal := range reader.ClassLiteralsIn(node) {
				for _, collapse := range shorthandCollapses(SplitClasses(literal.Text)) {
					if !shorthandsExistIn(collapse.shorthands, system) {
						continue
					}
					ctx.ReportRange(literal.Range, messageShorthandClasses(
						strings.Join(collapse.longhands, " "),
						strings.Join(collapse.shorthands, " "),
					))
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

// shorthandsExistIn reports whether every class a collapse would suggest is one the design system has.
func shorthandsExistIn(shorthands []string, system *tailwindengine.LoadedDesignSystem) bool {
	for _, shorthand := range shorthands {
		if !classExistsIn(shorthand, system) {
			return false
		}
	}
	return true
}

// shorthandCollapse is one reportable pair: the classes written and the class that replaces them.
type shorthandCollapse struct {
	longhands  []string
	shorthands []string
}

// dissectedClass is one class split into the parts a collapse has to match on.
type shorthandDissection struct {
	className string
	variants  string
	base      string
	negative  bool
	important bool
	markerEnd bool
}

// shorthandCollapses finds every collapse available in one class list, at most one per family.
func shorthandCollapses(classNames []string) []shorthandCollapse {
	dissected := make([]shorthandDissection, 0, len(classNames))
	for _, className := range classNames {
		dissected = append(dissected, dissectShorthandClass(className))
	}

	present := make(map[string]bool, len(classNames))
	for _, className := range classNames {
		present[className] = true
	}

	collapses := []shorthandCollapse{}
	for _, group := range shorthandGroups {
		if collapse, found := firstCollapseInGroup(group, dissected, present); found {
			collapses = append(collapses, collapse)
		}
	}
	return collapses
}

// firstCollapseInGroup returns the first rule in one family that matches, longest-first.
//
// Upstream sorts each family by pattern count descending and takes the first match, so a class list
// holding all four margin sides collapses to `m-1` rather than reporting the two-sided rules that
// also match.
func firstCollapseInGroup(group []shorthandRule, dissected []shorthandDissection,
	present map[string]bool) (shorthandCollapse, bool) {
	ordered := make([]shorthandRule, len(group))
	copy(ordered, group)
	for outer := 1; outer < len(ordered); outer++ {
		candidate := ordered[outer]
		inner := outer - 1
		for inner >= 0 && len(ordered[inner].Longhands) < len(candidate.Longhands) {
			ordered[inner+1] = ordered[inner]
			inner--
		}
		ordered[inner+1] = candidate
	}

	for _, shorthand := range ordered {
		if collapse, found := matchShorthandRule(shorthand, dissected, present); found {
			return collapse, true
		}
	}
	return shorthandCollapse{}, false
}

// matchShorthandRule looks for one rule's longhands among classes sharing a variant prefix.
func matchShorthandRule(shorthand shorthandRule, dissected []shorthandDissection,
	present map[string]bool) (shorthandCollapse, bool) {
	byVariants := map[string][]shorthandDissection{}
	order := []string{}
	for _, class := range dissected {
		if _, seen := byVariants[class.variants]; !seen {
			order = append(order, class.variants)
		}
		byVariants[class.variants] = append(byVariants[class.variants], class)
	}

	for _, variants := range order {
		collapse, found := matchWithinVariantGroup(shorthand, byVariants[variants], present)
		if found {
			return collapse, true
		}
	}
	return shorthandCollapse{}, false
}

// matchWithinVariantGroup matches one rule against classes that already share a variant prefix.
func matchWithinVariantGroup(shorthand shorthandRule, classes []shorthandDissection,
	present map[string]bool) (shorthandCollapse, bool) {
	matched := make([]shorthandDissection, 0, len(shorthand.Longhands))
	var captured []string

	for _, pattern := range shorthand.Longhands {
		expression := shorthandPattern(pattern)
		for _, class := range classes {
			groups := expression.FindStringSubmatch(class.base)
			if groups == nil {
				continue
			}
			if captured == nil {
				captured = groups
			} else {
				if len(groups) != len(captured) {
					continue
				}
				agrees := true
				for index := 1; index < len(groups); index++ {
					if groups[index] != captured[index] {
						agrees = false
						break
					}
				}
				if !agrees {
					continue
				}
			}
			matched = append(matched, class)
			break
		}
	}

	if len(matched) != len(shorthand.Longhands) {
		return shorthandCollapse{}, false
	}

	// The sign and the marker have to agree across the whole group, not merely be present on one.
	// Upstream checks each separately and each one silently widens the rule if dropped.
	markerEnd := false
	for _, class := range matched {
		if class.markerEnd {
			markerEnd = true
			break
		}
	}
	important := markerEnd
	if !important {
		for _, class := range matched {
			if class.important {
				important = true
				break
			}
		}
	}
	negative := false
	for _, class := range matched {
		if class.negative {
			negative = true
			break
		}
	}
	for _, class := range matched {
		if class.important != important && class.markerEnd != important {
			return shorthandCollapse{}, false
		}
		if class.negative != negative {
			return shorthandCollapse{}, false
		}
	}

	variants := matched[0].variants
	longhands := make([]string, 0, len(matched))
	for _, class := range matched {
		longhands = append(longhands, class.className)
	}

	shorthands := make([]string, 0, len(shorthand.Shorthands))
	for _, substitute := range shorthand.Shorthands {
		base := substitute
		for index := len(captured) - 1; index >= 1; index-- {
			base = strings.ReplaceAll(base, "$"+itoa(index), captured[index])
		}
		shorthands = append(shorthands, buildShorthandClass(variants, base, negative, important, markerEnd))
	}

	// Already written alongside the longhands, so there is nothing to say.
	allPresent := true
	for _, className := range shorthands {
		if !present[className] {
			allPresent = false
			break
		}
	}
	if allPresent {
		return shorthandCollapse{}, false
	}

	return shorthandCollapse{longhands: longhands, shorthands: shorthands}, true
}

// buildShorthandClass reassembles a class from the parts its group agreed on.
func buildShorthandClass(variants string, base string, negative bool, important bool, markerEnd bool) string {
	className := base
	if negative {
		className = "-" + className
	}
	if important {
		if markerEnd {
			className += "!"
		} else {
			className = "!" + className
		}
	}
	if variants != "" {
		className = variants + ":" + className
	}
	return className
}

// dissectShorthandClass splits a class into the parts a collapse matches on.
func dissectShorthandClass(className string) shorthandDissection {
	prefix, utility, hasVariants := splitVariantPrefix(className)
	if !hasVariants {
		utility = className
		prefix = ""
	}

	negative := strings.HasPrefix(utility, "-")
	base := strings.TrimPrefix(utility, "-")
	important := strings.HasPrefix(base, "!")
	base = strings.TrimPrefix(base, "!")
	markerEnd := strings.HasSuffix(base, "!")
	base = strings.TrimSuffix(base, "!")

	return shorthandDissection{
		className: className,
		variants:  prefix,
		base:      base,
		negative:  negative,
		important: important || markerEnd,
		markerEnd: markerEnd,
	}
}

// shorthandPatterns is the compiled table, built once at init.
//
// Built eagerly rather than memoized on first use, and that is a correctness fix rather than a
// preference. A lazily-filled map was the first version and it crashed the whole run with "concurrent
// map read and map write": `Graph.Walk` dispatches files across goroutines, so every rule's listener
// body runs in parallel with itself on different files. It failed one run in five, which is the
// worst rate to have — often enough to be real, rare enough to look like someone else's flake.
//
// The table is a package-level constant of 48 rules, so there is nothing to defer: compiling it at
// init costs one pass over a fixed list and removes the shared mutable state entirely, which is a
// better answer than a mutex around a cache that would never miss twice.
var shorthandPatterns = compileShorthandPatterns()

func compileShorthandPatterns() map[string]*regexp.Regexp {
	compiled := map[string]*regexp.Regexp{}
	for _, group := range shorthandGroups {
		for _, shorthand := range group {
			for _, pattern := range shorthand.Longhands {
				if _, isCompiled := compiled[pattern]; isCompiled {
					continue
				}
				compiled[pattern] = regexp.MustCompile("^" + pattern + "$")
			}
		}
	}
	return compiled
}

// shorthandPattern returns one table entry's compiled expression.
func shorthandPattern(pattern string) *regexp.Regexp {
	return shorthandPatterns[pattern]
}

// itoa is strconv.Itoa for the single digit a substitution placeholder can hold.
func itoa(value int) string {
	return string(rune('0' + value))
}
