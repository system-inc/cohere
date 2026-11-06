package policy

import (
	"fmt"
	"sort"
	"strings"
)

// RuleNaming is RuleNaming.json: the closed lists a house rule's name is built from.
type RuleNaming struct {
	About      string           `json:"about"`
	Verbs      []RuleNamingWord `json:"verbs"`
	Categories []RuleNamingWord `json:"categories"`

	// Namespaces are the namespaces whose rules are named by a closed list rather than by the scheme:
	// `adamic/invariant-mutable` keeps Adamic's own name, so one rule has one name across both projects,
	// and the guard still covers it because a name outside the list is refused.
	Namespaces []RuleNamingNamespace `json:"namespaces"`
}

// ParseHouseRuleName splits a house rule's name after its namespace (`consistency-no-print`, not
// `cohere-swift/consistency-no-print`) into category, verb and object, or says why it does not fit.
//
// Categories are tried longest first, so `react-hook-no-any-type` reads as `react-hook`, and a category
// that is a prefix of another can never claim the longer one's names.
func (naming RuleNaming) ParseHouseRuleName(leaf string) (HouseRuleName, error) {
	categories := make([]string, 0, len(naming.Categories))
	for _, category := range naming.Categories {
		categories = append(categories, category.Name)
	}
	sort.Slice(categories, func(left, right int) bool { return len(categories[left]) > len(categories[right]) })

	for _, category := range categories {
		rest, found := strings.CutPrefix(leaf, category+"-")
		if !found {
			continue
		}
		for _, verb := range naming.Verbs {
			object, found := strings.CutPrefix(rest, verb.Name+"-")
			if found && isKebabWord(object) {
				return HouseRuleName{Category: category, Verb: verb.Name, Object: object}, nil
			}
		}
		return HouseRuleName{}, fmt.Errorf(
			"%q has the category %q, and what follows is not a verb from the closed list and an object",
			leaf,
			category,
		)
	}
	return HouseRuleName{}, fmt.Errorf("%q does not begin with a category from the closed list", leaf)
}

// ParseRuleName judges a house rule's name in its namespace: against the closed list when the namespace
// has one, and by the scheme otherwise. A closed name reads as the namespace for its category and the
// whole name for its object, since it was never built from a verb.
func (naming RuleNaming) ParseRuleName(namespace string, leaf string) (HouseRuleName, error) {
	for _, closed := range naming.Namespaces {
		if closed.Name != namespace {
			continue
		}
		for _, name := range closed.Names {
			if name.Name == leaf {
				return HouseRuleName{Category: closed.Name, Object: leaf}, nil
			}
		}
		return HouseRuleName{}, fmt.Errorf(
			"%q is not in the %s namespace's closed list in RuleNaming.json",
			leaf,
			closed.Name,
		)
	}
	return naming.ParseHouseRuleName(leaf)
}
