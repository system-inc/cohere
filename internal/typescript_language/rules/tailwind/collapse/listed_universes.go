package tailwind

import "strings"

// listedUniverses hands out the shared property universes listed classes are grouped by, each a superset
// of every property a class reaching it can sign to under either feature set. The unbounded universe is
// nil: such a class is a possible match for every written class.
type listedUniverses struct {
	system    *LoadedDesignSystem
	byKey     map[string]*map[string]bool
	unbounded *map[string]bool
	// rootKeys remembers each functional root's keys, and colourKeys each root's colour keys as a set,
	// so a class costs map lookups rather than strings built and theme lookups made per class.
	rootKeys   map[string]rootKeys
	colourKeys map[string]map[string]bool
}

// of is the universe of one listed class: the union over its sources, each property with its
// expansions, since the index serves both feature sets.
func (universes *listedUniverses) of(entry ClassListEntry) *map[string]bool {
	key := ""
	for index, source := range entry.sources {
		part, bounded := universes.sourceKey(source)
		if !bounded {
			return universes.unbounded
		}
		if index == 0 {
			key = part
		} else {
			key += "\x00" + part
		}
	}
	if existing, found := universes.byKey[key]; found {
		return existing
	}
	maybe := map[string]bool{}
	for _, source := range entry.sources {
		for _, property := range universes.sourceProperties(source) {
			maybe[property] = true
			for _, longhand := range expandableLonghands(property, SignatureFeaturesFor(true, true)) {
				maybe[longhand] = true
			}
		}
	}
	universes.byKey[key] = &maybe
	return &maybe
}

// sourceKey names the universe a source reaches, shared by every class reaching it, or false when the
// source reaches none this port can bound.
func (universes *listedUniverses) sourceKey(source classListSource) (string, bool) {
	if source.static {
		_, isFramework := FrameworkStaticSuggested[source.root]
		if _, isFlattened := FrameworkStaticDeclarations[source.root]; isFramework && !isFlattened {
			return "", false
		}
		return "static " + source.root, true
	}
	keys, remembered := universes.rootKeys[source.root]
	if !remembered {
		keys = universes.computeRootKeys(source.root)
		universes.rootKeys[source.root] = keys
	}
	if !keys.bounded {
		return "", false
	}
	if universes.arm(source) == "colour" {
		return keys.colour, true
	}
	return keys.other, true
}

func (universes *listedUniverses) computeRootKeys(root string) rootKeys {
	key := "root " + root
	if universes.system.DeclaresFunctionalUtility(root) {
		key += " repository"
	}
	if frameworkFunctionalRoots[root] {
		if _, probed := FrameworkRootProperties[root]; !probed {
			return rootKeys{}
		}
		return rootKeys{other: key + " other", colour: key + " colour", bounded: true}
	}
	return rootKeys{other: key, colour: key, bounded: true}
}

// arm is the bucket a framework root's suggested value reaches, decided as RootProperties.armFor decides
// it for a parsed class.
func (universes *listedUniverses) arm(source classListSource) string {
	if source.value == "" {
		return "other"
	}
	if isColourKeyword(source.value) {
		return "colour"
	}
	root := strings.TrimPrefix(source.root, "-")
	colourKeys, built := universes.colourKeys[root]
	if !built {
		colourKeys = map[string]bool{}
		for _, key := range universes.system.Theme().KeysInNamespaces(colourNamespacesOf(root)) {
			colourKeys[legacyNumericKey.ReplaceAllString(key, "$1.$2")] = true
		}
		universes.colourKeys[root] = colourKeys
	}
	if colourKeys[source.value] {
		return "colour"
	}
	return "other"
}

// sourceProperties is every property a source's classes can declare, before expansion.
func (universes *listedUniverses) sourceProperties(source classListSource) []string {
	system := universes.system
	var properties []string
	if source.static {
		for _, declaration := range FrameworkStaticDeclarations[source.root] {
			properties = append(properties, declaration.Property)
		}
		return append(properties, bodyProperties(system.staticUtilityNodes[source.root])...)
	}
	if system.DeclaresFunctionalUtility(source.root) && system.utility != nil {
		if definition, defined := system.utility.Definitions[source.root]; defined {
			properties = append(properties, bodyProperties(definition.Nodes)...)
		}
	}
	if universe, probed := FrameworkRootProperties[source.root]; probed && frameworkFunctionalRoots[source.root] {
		if universes.arm(source) == "colour" {
			properties = append(properties, universe.Colour...)
		} else {
			properties = append(properties, universe.Other...)
		}
	}
	return properties
}
