package tailwind

import "sort"

// LoadedDesignSystem is one repository's resolved design system, immutable once built.
//
// It satisfies the DesignSystem interface in candidate.go, which is the interface ParseCandidate
// asks its four questions through. That interface is deliberately narrow and this type is
// deliberately wider: the parser needs four answers, and a rule computing a sort key needs the
// theme, the descriptor tables and the variant registry as well.
//
// Safe for concurrent reads and never written after LoadDesignSystem returns. See the file comment
// for why that is a hard requirement rather than a nicety.
type LoadedDesignSystem struct {
	// EntryPoint is the stylesheet the load started from, absolute.
	EntryPoint string

	// TailwindVersion is the engine version the generated tables were produced against.
	//
	// Carried so a mismatch between the tables and an upgraded `tailwindcss` is a loud failure
	// rather than a subtly wrong sort. Nothing verifies it yet; #4q5dsn3 is where the sort starts
	// depending on it.
	TailwindVersion string

	// Stylesheets is every file the `@import` graph reached, absolute, in the order they were read.
	//
	// Reported rather than discarded because it is the invalidation set. A findings cache keyed on
	// a `.tsx` file's hash is stale the moment any of these changes, and a caller that cannot see
	// the list cannot know that. It is also what makes a load auditable: a repository whose theme
	// looks wrong is usually a repository whose graph reached a file nobody expected.
	Stylesheets []string

	// SkippedDirectives is every `@config` and `@plugin` the graph held.
	//
	// A `@config` can contribute theme values through JavaScript, which cohere does not run, so a
	// non-empty list means the theme may be short by however many keys that config declared. Kept
	// so a caller can decide, rather than dropped so a caller cannot.
	SkippedDirectives []SkippedDirective

	// BuildCount is how many times a design system has been built in this process.
	//
	// The whole point of this field is that it is observable from a test. See the file comment: the
	// per-file rebuild is a failure mode with no symptom other than time, and the rule that already
	// suffered it had a comment claiming it did not. A comment cannot fail; this can.
	BuildCount int

	theme    *Theme
	variants *VariantRegistry
	utility  *UtilityEvaluator

	// staticUtilityNodes is the body of every static `@utility` block the repository declared,
	// keyed by root.
	//
	// Kept because a static utility's reading is a constant PropertySort answers from its body, and
	// there is nowhere else to get it: the framework's static table does not know a name the
	// repository invented. This is the whole of what NewTable adds to `Statics`.
	staticUtilityNodes map[string][]*Node
	// utilityRoots is every `@utility` root the repository declared, mapped to the kinds it was
	// declared as, so HasUtility is a map lookup rather than a walk.
	//
	// A set of kinds rather than one kind, because a root can be declared both ways and sixteen in
	// this repository are: `@utility fade-in` and `@utility fade-in-*` are two blocks naming one
	// root, a static form taking no value and a functional form taking one. Storing a single kind
	// let the second block overwrite the first, which lost the static form silently and left
	// `fade-in` answerable by neither the table nor the evaluator.
	utilityRoots map[string]map[UtilityKind]bool
	// redeclaredUtilityRoots are the roots the repository declares in two `@utility` blocks of the same
	// kind. Upstream registers both and compiles and suggests both; UtilityEvaluator keeps the last, so a
	// caller that needs every class or every body of such a root has to know it is missing one.
	redeclaredUtilityRoots map[string]bool
	// customVariants is every `@custom-variant` name the repository declared.
	customVariants map[string]bool
	// frameworkVariants is the set of names the framework registered, so Contributions can tell an
	// appended registration from an in-place update without re-deriving the split.
	frameworkVariants map[string]bool
}

// Theme is the resolved theme, for reading.
//
// Returned by value-of-pointer because Theme's read methods are all on the pointer, and copying it
// would copy a map header rather than the map. The contract is the doc comment rather than the
// type: a caller that calls Add or ClearNamespace on this has broken the run for every other
// goroutine, and Go has no way to say so in the signature.
//
// `MarkUsedVariable` is the one read-shaped method that writes, and it is the reason this is named
// rather than embedded: a caller reaching for it has to type the reach and can be found by grep.
func (system *LoadedDesignSystem) Theme() *Theme { return system.theme }

// Variants is the variant registry, for reading.
func (system *LoadedDesignSystem) Variants() *VariantRegistry { return system.variants }

// Utilities is the `@utility` evaluator over this repository's own blocks.
//
// Nil when the repository declared no `@utility` blocks, which is a real state rather than a
// failure: most repositories declare none, and a caller must handle the nil rather than assume a
// block exists to evaluate.
func (system *LoadedDesignSystem) Utilities() *UtilityEvaluator { return system.utility }

// Prefix is the configured utility prefix, empty when there is none.
//
// Part of the DesignSystem interface candidate.go declares.
func (system *LoadedDesignSystem) Prefix() string { return system.theme.Prefix }

// HasUtility reports whether root is registered as a utility of the given kind.
//
// The repository's own `@utility` blocks are asked first, then the framework's registry. That order
// is upstream's: `@utility` registration runs after the framework's and `Utilities.set` overwrites,
// so a repository redefining a framework root wins. Both are consulted because neither alone is
// right: the framework tables do not know about `slide-in-from-top`, and the repository's blocks
// do not know about `bg`.
//
// A repository `@utility` block is always functional or static according to whether its name
// carries the `-*` suffix, which is what collectStylesheets recorded when it parsed the block.
func (system *LoadedDesignSystem) HasUtility(root string, kind UtilityKind) bool {
	if declared, isDeclared := system.utilityRoots[root]; isDeclared {
		return declared[kind]
	}
	switch kind {
	case UtilityKindStatic:
		_, registered := FrameworkStaticDeclarations[root]
		return registered
	case UtilityKindFunctional:
		return frameworkFunctionalRoots[root]
	default:
		return false
	}
}

// RepositoryStaticUtilityNames is every static utility the repository itself declares.
//
// Exposed for the upstream check, which compares the installed engine's registry against the
// checked-in framework tables and must subtract the repository's own blocks first. Reading them from
// `utilityRoots` rather than from the engine is what makes that subtraction exact: this is the set
// `collectStylesheets` recorded while parsing the repository's `@utility` blocks, so it cannot
// accidentally include a framework name.
func (system *LoadedDesignSystem) RepositoryStaticUtilityNames() []string {
	names := make([]string, 0, len(system.staticUtilityNodes))
	for name, kinds := range system.utilityRoots {
		if kinds[UtilityKindStatic] {
			names = append(names, name)
		}
	}
	return names
}

// DeclaresFunctionalUtility reports whether the repository itself declared this functional root.
//
// Narrower than `HasUtility`, and the difference is the whole point: `HasUtility` answers whether a
// root exists at all, framework roots included, so it says yes to `text` and `border`. This answers
// only what `collectStylesheets` recorded while parsing the repository's own `@utility` blocks.
//
// Needed by any caller that must treat a repository root differently from a framework one.
// `ClassValueResolvesIn` is the case: a framework root has a checked-in description saying how its
// values resolve, and a repository root has an `@utility` block the evaluator compiles instead, so
// asking a framework description about `shadow--3` would report a working class.
func (system *LoadedDesignSystem) DeclaresFunctionalUtility(root string) bool {
	return system.utilityRoots[root][UtilityKindFunctional]
}

// HasVariant reports whether root is a registered variant.
//
// The registry holds the framework's registrations and every `@custom-variant` the repository
// added, because LoadDesignSystem registered both into it. A `@custom-variant` reusing a framework
// name does not appear twice: `Register` updates in place and leaves the order untouched, which is
// upstream's behaviour and the reason both corpus repositories have registries byte-identical to
// the framework's despite redefining `dark`.
func (system *LoadedDesignSystem) HasVariant(root string) bool {
	return system.variants.Has(root)
}

// VariantKind returns the registration kind of a variant root.
//
// Only called for roots HasVariant accepted, per the interface's contract. An unregistered root
// answers static rather than panicking, because a parser asking about a root it was told exists is
// a bug in the caller and a panic inside a lint run costs a file rather than surfacing it.
func (system *LoadedDesignSystem) VariantKind(root string) ParsedVariantKind {
	registration, isRegistered := system.variants.Get(root)
	if !isRegistered {
		return ParsedVariantKindStatic
	}
	return registration.Kind
}

// VariantCompoundsWith reports whether the compound variant parent accepts the rules child
// produces.
//
// **The one method here that approximates rather than composes, and the child argument is ignored.**
// Upstream answers this from a bitwise-and of two `compounds` bitmasks: the parent's, assigned at
// registration in `variants.ts`, and the child's, which for an arbitrary variant is computed from
// its selector instead of looked up. Neither half is reachable from shipped Go. The masks are not in
// any generated table, and `compoundsForSelectors` (the selector computation) is ported only
// inside `candidate_test.go`, where the shipped path cannot call it.
//
// So this answers the parent's half alone: a compound variant accepts a child, anything else
// accepts nothing. That is upstream's answer whenever the child produces style rules, which is the
// overwhelming majority, and it is wrong in one direction for one shape: a compound whose parent
// accepts only style rules wrapping an at-rule-only child, `group-[@media(print)]` and its kin,
// where upstream refuses and this accepts.
//
// Written out rather than left as a bare `return true` because a caller should find the gap by
// reading the method, not by chasing a wrong sort. Closing it needs a `compounds` column on the
// variant table and `compoundsForSelectors` moved into the package proper, both of which are
// variant work rather than seam work. Nothing reads this yet: #4q5dsn3 and #3r6cxrb are where rules
// begin to.
func (system *LoadedDesignSystem) VariantCompoundsWith(parent string, _ ParsedVariant) bool {
	registration, isRegistered := system.variants.Get(parent)
	if !isRegistered {
		return false
	}
	// A compound variant is the only kind that can wrap another at all.
	return registration.Kind == ParsedVariantKindCompound
}

// Contributions reports what the repository added, for tests and for `--timing`-style reporting.
func (system *LoadedDesignSystem) Contributions() RepositoryContributions {
	newNames := []string{}
	for name := range system.customVariants {
		if !system.frameworkVariants[name] {
			newNames = append(newNames, name)
		}
	}
	sort.Strings(newNames)

	return RepositoryContributions{
		ThemeEntries:    system.theme.Size(),
		UtilityBlocks:   len(system.utilityRoots),
		CustomVariants:  len(system.customVariants),
		NewVariantNames: newNames,
		StylesheetCount: len(system.Stylesheets),
	}
}

// suggestionGroups is every functional root's groups in this design system: the framework's, with
// each repository `@utility` root's appended, as upstream's `suggest` appends a second registration's
// groups to the first.
func (system *LoadedDesignSystem) suggestionGroups() map[string][]SuggestionGroup {
	groups := make(map[string][]SuggestionGroup, len(FrameworkSuggestions))
	for root, framework := range FrameworkSuggestions {
		groups[root] = framework
	}
	if system.utility == nil {
		return groups
	}
	for root, definition := range system.utility.Definitions {
		groups[root] = append(append([]SuggestionGroup(nil), groups[root]...), repositorySuggestionGroup(definition))
	}
	return groups
}
