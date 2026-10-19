// Tailwind's class list for one design system: upstream's `getClassList`, built in Go from the
// framework's suggestion recipes and the run's own theme (#15swfzq unit 1b).
//
// The class list is the population upstream's arbitraryUtilities pass searches. It precomputes a
// signature for every class the list holds, and a written `bg-(--color-x)` is rewritten to the one
// listed class sharing its signature, or left alone when two do. So which classes are listed decides
// what is reported, in both directions: a class missing here can turn upstream's bail into a
// rewrite, and a class listed here that upstream does not list can turn its rewrite into a bail.
//
// # Where each half comes from
//
// A functional utility's suggestions are a recipe: literal values, theme namespaces whose keys are
// offered, the fractions, a bare default, and the same for its modifiers. The recipe is a fact about
// Tailwind and is generated, as FrameworkSuggestions, by probing the engine with a theme of one key
// per namespace. The keys are a fact about the repository, so they are read here, from the run's
// Theme, through the same `keysInNamespaces` upstream calls. A repository's own `@utility` blocks
// carry recipes of their own, which upstream records while registering them; that tracking is
// repositorySuggestionGroups.
//
// TestClassListAgreesWithTailwind holds the result to `getClassList()` on the public theme, ahra and
// www-connected-app, entry for entry, modifiers included.
package tailwind

import (
	"regexp"
	"sort"
	"strings"
)

// SuggestionList is one side of a suggestion group, values or modifiers: upstream's
// SuggestionDefinition after `suggest` resolves it, kept as the recipe rather than the result.
type SuggestionList struct {
	// Literals are the values the registration names outright.
	Literals []string
	// SpacingScale is whether DefaultSpacingSuggestions follow the literals. Upstream offers them
	// only when the theme sets `--spacing`, tested for truth: `theme.get(['--spacing']) ? ... : []`.
	SpacingScale bool
	// Namespaces are the theme namespaces whose keys are offered, through keysInNamespaces.
	Namespaces []string
	// Fractions is whether SuggestedFractions are offered after everything else.
	Fractions bool
	// HasDefault is whether the bare root is offered, upstream's `hasDefaultValue`.
	HasDefault bool
	// DefaultWhenThemeKey, when set, is the key HasDefault depends on: the shadow family offers its
	// bare root only when `theme.get([key]) !== null`, a presence test.
	DefaultWhenThemeKey string
}

// SuggestionGroup is one group a functional root suggests, upstream's SuggestionGroup.
type SuggestionGroup struct {
	// SupportsNegative is whether each value is offered with a leading `-` too.
	SupportsNegative bool
	// Values are the values, each making a class `root-value`, or `root` for the default.
	Values SuggestionList
	// Modifiers are the modifiers every class this group makes accepts.
	Modifiers SuggestionList
}

// ClassListEntry is one class the class list holds, with the modifiers it is offered with.
type ClassListEntry struct {
	// Name is the class, without a prefix.
	Name string
	// Modifiers are the modifiers offered with it, deduplicated, in the order first offered.
	Modifiers []string
}

// legacyNumericKey is upstream's LEGACY_NUMERIC_KEY: a theme key spelled `2_5` is suggested as `2.5`.
var legacyNumericKey = regexp.MustCompile(`(\d+)_(\d+)`)

// BuildClassList is upstream's getClassList over this design system, sorted by name.
//
// Upstream keys its entries by name in a map, statics first and then each functional root's groups,
// so a static and a functional suggestion of the same name are one entry whose modifiers are the
// union. That is kept; upstream's own sort, a natural compare with fractions moved last, is not,
// because nothing that reads the list here reads its order.
func BuildClassList(system *LoadedDesignSystem) []ClassListEntry {
	entries := map[string]*ClassListEntry{}
	entry := func(name string) *ClassListEntry {
		existing, found := entries[name]
		if !found {
			existing = &ClassListEntry{Name: name}
			entries[name] = existing
		}
		return existing
	}

	// Statics: every one the registry holds, less those whose `suggest` returns nothing.
	for name, suggested := range FrameworkStaticSuggested {
		if suggested {
			entry(name)
		}
	}
	// A repository static is listed the same way: upstream asks the root's completions, and a
	// framework `suggest` that returns nothing still answers for a name the repository redeclares.
	for name, kinds := range system.utilityRoots {
		if suggested, isFramework := FrameworkStaticSuggested[name]; kinds[UtilityKindStatic] && (suggested || !isFramework) {
			entry(name)
		}
	}

	theme := system.Theme()
	for root, groups := range system.suggestionGroups() {
		for _, group := range groups {
			modifiers := suggestionValues(group.Modifiers, theme)
			for _, value := range suggestionValues(group.Values, theme) {
				name := root
				if value != "" {
					name = root + "-" + value
				}
				addModifiers(entry(name), modifiers)
				if group.SupportsNegative {
					addModifiers(entry("-"+name), modifiers)
				}
			}
		}
	}

	list := make([]ClassListEntry, 0, len(entries))
	for _, listed := range entries {
		list = append(list, *listed)
	}
	sort.Slice(list, func(left, right int) bool { return list[left].Name < list[right].Name })
	return list
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

// suggestionValues is one list resolved against the theme, in upstream's order: the default as the
// empty string first, then the literals, the spacing scale, the theme's keys and the fractions.
func suggestionValues(list SuggestionList, theme *Theme) []string {
	var values []string
	if list.HasDefault {
		if list.DefaultWhenThemeKey == "" {
			values = append(values, "")
		} else if _, present := theme.Get([]string{list.DefaultWhenThemeKey}); present {
			values = append(values, "")
		}
	}
	values = append(values, list.Literals...)
	if list.SpacingScale {
		if spacing, present := theme.Get([]string{"--spacing"}); present && spacing != "" {
			values = append(values, DefaultSpacingSuggestions...)
		}
	}
	for _, key := range theme.KeysInNamespaces(list.Namespaces) {
		values = append(values, legacyNumericKey.ReplaceAllString(key, "$1.$2"))
	}
	if list.Fractions {
		values = append(values, SuggestedFractions...)
	}
	return values
}

// addModifiers merges modifiers into an entry, keeping the first occurrence of each.
func addModifiers(entry *ClassListEntry, modifiers []string) {
	for _, modifier := range modifiers {
		found := false
		for _, existing := range entry.Modifiers {
			if existing == modifier {
				found = true
				break
			}
		}
		if !found {
			entry.Modifiers = append(entry.Modifiers, modifier)
		}
	}
}

// repositorySuggestionGroup is the one group a repository's functional `@utility` block suggests:
// upstream's createCssUtility, which tracks what each `--value()` and `--modifier()` names while it
// normalizes them and registers the result.
//
// The tracking reads the normalized body, which is what NewUtilityEvaluator left in the definition,
// so an argument is already in the form upstream tracks it in.
//
// One definition per root, because UtilityEvaluator keeps one: a repository declaring `@utility foo-*`
// twice registers two groups upstream and one here.
func repositorySuggestionGroup(definition *UtilityDefinition) SuggestionGroup {
	tracked := map[string]*repositorySuggestionTracking{
		"--value":    {},
		"--modifier": {},
	}
	var visit func(nodes []*Node)
	visit = func(nodes []*Node) {
		for _, node := range nodes {
			if len(node.Nodes) > 0 {
				visit(node.Nodes)
			}
			if node.Kind != KindDeclaration || !node.ValuePresent || node.Value == "" {
				continue
			}
			if !strings.Contains(node.Value, "--value(") && !strings.Contains(node.Value, "--modifier(") {
				continue
			}
			trackValueFunctions(ParseValue(node.Value), tracked)
		}
	}
	visit(definition.Nodes)

	return SuggestionGroup{
		Values:    tracked["--value"].list(),
		Modifiers: tracked["--modifier"].list(),
	}
}

// repositorySuggestionTracking is upstream's per-function storage: what one of `--value` and
// `--modifier` was seen naming.
type repositorySuggestionTracking struct {
	usedSpacingInteger bool
	usedSpacingNumber  bool
	namespaces         []string
	literals           []string
}

// list is the tracking as a recipe. The spacing scale goes in as literals, the whole scale for a
// `number` argument and its integers for an `integer` one, since unlike the framework's it does not
// depend on the theme.
func (tracking *repositorySuggestionTracking) list() SuggestionList {
	list := SuggestionList{Literals: append([]string(nil), tracking.literals...), Namespaces: tracking.namespaces}
	if tracking.usedSpacingNumber {
		list.Literals = append(list.Literals, DefaultSpacingSuggestions...)
	} else if tracking.usedSpacingInteger {
		for _, value := range DefaultSpacingSuggestions {
			if isPositiveInteger(value) {
				list.Literals = append(list.Literals, value)
			}
		}
	}
	return list
}

// trackValueFunctions walks one declaration value as upstream's tracking walk does: a `--spacing()`
// call records the `integer` or `number` its `--value()` or `--modifier()` names and is then walked
// into as well, and each `--value()` or `--modifier()` records its quoted literals and its theme keys.
func trackValueFunctions(nodes []ValueNode, tracked map[string]*repositorySuggestionTracking) {
	for index := range nodes {
		node := &nodes[index]
		if node.Kind != ValueNodeKindFunction {
			continue
		}
		switch node.Value {
		case "--spacing":
			trackSpacingArguments(node.Nodes, tracked)
			trackValueFunctions(node.Nodes, tracked)
		case "--value", "--modifier":
			tracking := tracked[node.Value]
			for _, argument := range node.Nodes {
				if argument.Kind != ValueNodeKindWord || argument.Value == "" {
					continue
				}
				word := argument.Value
				if (word[0] == '"' || word[0] == '\'') && len(word) >= 2 && word[len(word)-1] == word[0] {
					tracking.literals = appendUnique(tracking.literals, word[1:len(word)-1])
				} else if strings.HasPrefix(word, "--") {
					namespace := word
					if star := strings.Index(word, "-*"); star != -1 {
						namespace = word[:star]
					}
					tracking.namespaces = appendUnique(tracking.namespaces, namespace)
				}
			}
		default:
			trackValueFunctions(node.Nodes, tracked)
		}
	}
}

// trackSpacingArguments records, for each `--value()` or `--modifier()` inside a `--spacing()` call,
// whether it names `integer` or `number`.
func trackSpacingArguments(nodes []ValueNode, tracked map[string]*repositorySuggestionTracking) {
	for index := range nodes {
		node := &nodes[index]
		if node.Kind != ValueNodeKindFunction {
			continue
		}
		if node.Value != "--value" && node.Value != "--modifier" {
			trackSpacingArguments(node.Nodes, tracked)
			continue
		}
		for _, argument := range node.Nodes {
			if argument.Kind != ValueNodeKindWord {
				continue
			}
			switch argument.Value {
			case "integer":
				tracked[node.Value].usedSpacingInteger = true
			case "number":
				tracked[node.Value].usedSpacingNumber = true
			}
		}
	}
}

// appendUnique appends value unless the slice already holds it, keeping a Set's insertion order.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
