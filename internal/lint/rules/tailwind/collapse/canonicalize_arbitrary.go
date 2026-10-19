// arbitraryUtilities and the three utility passes upstream runs before it, ported over the signature
// (#15swfzq unit 1b).
//
// arbitraryUtilities rewrites a class written with an arbitrary value, `ring-(--color-content-1)` or
// `z-[1]`, to the listed class that compiles to the same signature, `ring-content-1` or `z-1`. It searches
// the whole class list for that signature. Two non-negative listed classes sharing it make it bail. None
// sends it through a fallback that tries every functional root with the value as a bare value, as a
// spacing multiple and as an arbitrary value.
//
// # The two relations, and which way each errs
//
// Every comparison here is one of two, kept apart by name:
//
//   - exact-equal, which decides a rewrite. Two classes are the same only when both signatures are
//     SignatureExact and their strings are equal. It errs toward not equal, so it can only miss a rewrite.
//   - possible-match, which decides whether anything could stand in the way. A listed class whose
//     signature is unknown is a possible match when its SignatureBound admits the written class's
//     properties. Any possible match the pass cannot rule out leaves the class as written. It errs toward
//     admitting, so it can only bail where upstream rewrites.
//
// So the pass rewrites only when it knows every class upstream would have weighed. Where it does not, it
// answers false and no pass after it runs.
//
// # Searching only where a match can be
//
// Upstream signs all 38,000 listed classes up front. This signs only the classes whose property universe
// could hold the written class's properties. classListIndex groups the list by that universe once per
// design system, and a pool is built per property set, per feature set, the first time a written class
// asks for it.
package tailwind

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// canonicalizerShared is what every feature set's Canonicalizer over one design system has in common.
type canonicalizerShared struct {
	system     *LoadedDesignSystem
	byFeatures sync.Map
	indexOnce  sync.Once
	index      *classListIndex
}

// classListIndex is the class list arranged for searching, built the first time a written class needs it.
type classListIndex struct {
	// compiler signs classes, with `@theme inline` off as upstream's signature compile has it.
	compiler *SignatureCompiler
	// rawCompiler compiles a class as `candidatesToCss` does, with the theme as declared, which is what
	// allVariablesAreUsed reads.
	rawCompiler *SignatureCompiler
	// groups are the listed classes, gathered by the properties their signatures can declare.
	groups []*classListGroup
	// redeclared is whether the repository declares one `@utility` root twice. UtilityEvaluator keeps one
	// definition where upstream registers both and suggests both groups, so such a list is missing
	// classes, and a missing class can turn upstream's bail into a rewrite here. The pass declines.
	redeclared bool
	// roots are every functional root the fallback tries, framework and repository.
	roots []string
	// spacing is the theme's `--spacing`, folded and parsed, or nil when it is absent or not a dimension.
	spacing *dimension
}

// classListGroup is listed classes whose signatures can only declare properties in maybe.
type classListGroup struct {
	maybe   map[string]bool
	classes []string
}

// classIndex is the index, built once per design system.
func (shared *canonicalizerShared) classIndex() *classListIndex {
	shared.indexOnce.Do(func() {
		shared.index = buildClassListIndex(shared.system)
	})
	return shared.index
}

// buildClassListIndex arranges one design system's class list for the pass.
//
// The population is upstream's precomputed lookup: every listed class, and each one with every modifier
// it is offered with that is not a number, since upstream skips those as computable. The `-x-0` that
// upstream drops when it signs the same as `x-0` stays here; the pool drops it, where both are signed.
func buildClassListIndex(system *LoadedDesignSystem) *classListIndex {
	index := &classListIndex{
		compiler:    NewSignatureCompiler(system),
		rawCompiler: &SignatureCompiler{system: system, compileTheme: system.Theme(), utilities: system.utility},
		redeclared:  len(system.redeclaredUtilityRoots) > 0,
	}
	for root := range frameworkFunctionalRoots {
		index.roots = append(index.roots, root)
	}
	for root, kinds := range system.utilityRoots {
		if kinds[UtilityKindFunctional] && !frameworkFunctionalRoots[root] {
			index.roots = append(index.roots, root)
		}
	}
	sort.Strings(index.roots)

	if spacing, found := resolveThemeValue("--spacing", system.Theme()); found {
		index.spacing = parseDimension(constantFoldDeclaration(spacing, nil))
	}

	// Each class goes to the group of the universe its source can reach, with no class parsed or
	// compiled: a static's body, a framework root's probed universe for the arm its value takes, and a
	// repository root's body. Universes are shared, so a group is a universe and the classes in it.
	groups := map[*map[string]bool]*classListGroup{}
	universes := &listedUniverses{
		system:     system,
		byKey:      map[string]*map[string]bool{},
		unbounded:  new(map[string]bool),
		rootKeys:   map[string]rootKeys{},
		colourKeys: map[string]map[string]bool{},
	}
	// Whether a modifier is a number upstream computes rather than lists, asked once per modifier: the
	// same twenty-one opacities follow every colour.
	computable := map[string]bool{}
	for _, entry := range BuildClassList(system) {
		maybe := universes.of(entry)
		group, found := groups[maybe]
		if !found {
			group = &classListGroup{maybe: *maybe}
			groups[maybe] = group
		}
		group.classes = append(group.classes, entry.Name)
		for _, modifier := range entry.Modifiers {
			isComputable, asked := computable[modifier]
			if !asked {
				isComputable = isValidSpacingMultiplier(modifier)
				computable[modifier] = isComputable
			}
			if !isComputable {
				group.classes = append(group.classes, entry.Name+"/"+modifier)
			}
		}
	}
	for _, group := range groups {
		index.groups = append(index.groups, group)
	}
	return index
}

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

// rootKeys are the two keys a functional root's sources reach, one per arm, built once per root.
type rootKeys struct {
	other   string
	colour  string
	bounded bool
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

// bodyProperties is every property a `@utility` body declares, at any depth: a superset of what any of
// its classes compiles to, since a `--value()` resolving nothing drops a declaration and never adds one.
func bodyProperties(nodes []*Node) []string {
	var properties []string
	for _, node := range nodes {
		if node.Kind == KindDeclaration {
			properties = append(properties, node.Property)
		}
		properties = append(properties, bodyProperties(node.Nodes)...)
	}
	return properties
}

// propertySetKey is a set of properties as a map key; nil, an unbounded set, has its own.
func propertySetKey(properties map[string]bool) string {
	if properties == nil {
		return "\x00unbounded"
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

// signaturePool is what the class list holds for one set of properties under one feature set.
type signaturePool struct {
	// exact maps each signature to the listed classes that sign to it exactly.
	exact map[string][]string
	// uncertain is whether some listed class is a possible match that could not be signed.
	uncertain bool
}

// classSignature is a class's signature under this canonicalizer's features, remembered.
func (canonicalizer *Canonicalizer) classSignature(className string) (string, SignatureState) {
	type signed struct {
		signature string
		state     SignatureState
	}
	if remembered, found := canonicalizer.signatures.Load(className); found {
		answer := remembered.(signed)
		return answer.signature, answer.state
	}
	signature, state := canonicalizer.shared.classIndex().compiler.ClassSignature(className, canonicalizer.features)
	canonicalizer.signatures.Store(className, signed{signature, state})
	return signature, state
}

// pool is the signature pool for one property set, built the first time it is asked for.
func (canonicalizer *Canonicalizer) pool(properties map[string]bool) *signaturePool {
	key := propertySetKey(properties)
	if remembered, found := canonicalizer.pools.Load(key); found {
		return remembered.(*signaturePool)
	}
	index := canonicalizer.shared.classIndex()
	pool := &signaturePool{exact: map[string][]string{}}
	for _, group := range index.groups {
		if !groupAdmits(group, properties) {
			continue
		}
		for _, className := range group.classes {
			signature, state := canonicalizer.classSignature(className)
			switch state {
			case SignatureExact:
				if canonicalizer.isDroppedNegativeZero(className, signature) {
					continue
				}
				pool.exact[signature] = append(pool.exact[signature], className)
			case SignatureUnknown:
				// possible-match: an unknown listed class counts whenever its bound admits the properties.
				if index.compiler.ClassSignatureBound(className, canonicalizer.features).Admits(properties) {
					pool.uncertain = true
				}
			}
		}
	}
	stored, _ := canonicalizer.pools.LoadOrStore(key, pool)
	return stored.(*signaturePool)
}

// groupAdmits is the possible-match relation at group level: a group can hold a class signing to these
// properties only when its universe holds them all. An unbounded group admits everything.
func groupAdmits(group *classListGroup, properties map[string]bool) bool {
	if group.maybe == nil {
		return true
	}
	for property := range properties {
		if !group.maybe[property] {
			return false
		}
	}
	return true
}

// isDroppedNegativeZero is upstream's skip: `-x-0` is left out of the lookup when it signs the same as
// `x-0`, so the positive spelling is preferred. Only when both are signed exactly; an unknown `x-0` keeps
// `-x-0` in, which can only add a negative match, and negatives never decide a rewrite.
func (canonicalizer *Canonicalizer) isDroppedNegativeZero(className string, signature string) bool {
	if !strings.HasPrefix(className, "-") || !strings.HasSuffix(className, "-0") {
		return false
	}
	positive, state := canonicalizer.classSignature(className[1:])
	return state == SignatureExact && positive == signature
}

// themeToVarUtilityGuard stands where upstream's themeToVarUtility runs. That pass rewrites a legacy
// `theme()` call in an arbitrary value to `var()`, and is not ported, so a value holding one stops the
// passes rather than reaching the ones after it in a form upstream's never have.
func (canonicalizer *Canonicalizer) themeToVarUtilityGuard(candidate ParsedCandidate) (ParsedCandidate, bool) {
	value, isArbitrary := arbitraryValueOf(&candidate)
	if !isArbitrary {
		return candidate, true
	}
	return candidate, !holdsFunction(ParseValue(value), "theme")
}

// arbitraryValueOf is the arbitrary value a candidate carries: an arbitrary property's value or a
// functional utility's arbitrary value.
func arbitraryValueOf(candidate *ParsedCandidate) (string, bool) {
	switch {
	case candidate.Kind == ParsedCandidateKindArbitrary:
		return candidate.PropertyValue, true
	case candidate.Kind == ParsedCandidateKindFunctional && candidate.Value != nil && candidate.Value.Kind == ParsedValueKindArbitrary:
		return candidate.Value.Value, true
	}
	return "", false
}

// setArbitraryValue writes an arbitrary value back where arbitraryValueOf read it.
func setArbitraryValue(candidate *ParsedCandidate, value string) {
	if candidate.Kind == ParsedCandidateKindArbitrary {
		candidate.PropertyValue = value
		return
	}
	copied := *candidate.Value
	copied.Value = value
	candidate.Value = &copied
}

// holdsFunction reports whether a value calls the named function anywhere.
func holdsFunction(nodes []ValueNode, name string) bool {
	for index := range nodes {
		if nodes[index].Kind == ValueNodeKindFunction && nodes[index].Value == name {
			return true
		}
		if holdsFunction(nodes[index].Nodes, name) {
			return true
		}
	}
	return false
}

// calcToSpacingFunction is upstream's pass of the same name: `calc(var(--spacing) * n)` in an arbitrary
// value becomes `--spacing(n)`.
func (canonicalizer *Canonicalizer) calcToSpacingFunction(candidate ParsedCandidate) (ParsedCandidate, bool) {
	value, isArbitrary := arbitraryValueOf(&candidate)
	if !isArbitrary {
		return candidate, true
	}
	spacingVariable := "--spacing"
	if prefix := canonicalizer.system.Prefix(); prefix != "" {
		spacingVariable = "--" + prefix + "-spacing"
	}
	var walk func(nodes []ValueNode) []ValueNode
	walk = func(nodes []ValueNode) []ValueNode {
		for index := 0; index < len(nodes); {
			node := &nodes[index]
			if node.Kind == ValueNodeKindFunction && node.Value == "calc" && len(node.Nodes) == 5 &&
				node.Nodes[2].Kind == ValueNodeKindWord && node.Nodes[2].Value == "*" &&
				node.Nodes[0].Kind == ValueNodeKindFunction && node.Nodes[0].Value == "var" &&
				len(node.Nodes[0].Nodes) == 1 && node.Nodes[0].Nodes[0].Kind == ValueNodeKindWord &&
				node.Nodes[0].Nodes[0].Value == spacingVariable {
				replacement := ParseValue("--spacing(" + ValueToCss(node.Nodes[4:5]) + ")")
				nodes = append(nodes[:index:index], append(replacement, nodes[index+1:]...)...)
				continue
			}
			if len(node.Nodes) > 0 {
				node.Nodes = walk(node.Nodes)
			}
			index++
		}
		return nodes
	}
	setArbitraryValue(&candidate, ValueToCss(walk(ParseValue(value))))
	return candidate, true
}

// optimizeArbitraryValueExpressions is upstream's pass of the same name for a functional utility's
// arbitrary value: a lone `calc()` folded, a negative root's sign moved into the value, and a `* -1`
// moved out into the root, each kept only when it signs the same as before.
func (canonicalizer *Canonicalizer) optimizeArbitraryValueExpressions(candidate ParsedCandidate) (ParsedCandidate, bool) {
	if candidate.Kind != ParsedCandidateKindFunctional || candidate.Value == nil || candidate.Value.Kind != ParsedValueKindArbitrary {
		return candidate, true
	}
	valueAst := ParseValue(candidate.Value.Value)
	isLone := func(nodes []ValueNode, name string) bool {
		return len(nodes) == 1 && nodes[0].Kind == ValueNodeKindFunction && nodes[0].Value == name
	}

	if isLone(valueAst, "calc") {
		if folded, foldedAst := constantFoldDeclarationAst(ParseValue(candidate.Value.Value), nil, false); folded {
			replacement := candidate
			setArbitraryValue(&replacement, ValueToCss(foldedAst))
			same, known := canonicalizer.hasSameSignature(candidate, replacement)
			if !known {
				return candidate, false
			}
			if same {
				candidate, valueAst = replacement, foldedAst
			}
		}
	}

	if strings.HasPrefix(candidate.Root, "-") {
		if isLone(valueAst, "var") {
			return candidate, true
		}
		expression := ParseValue("calc(" + candidate.Value.Value + " * -1)")
		if folded, foldedAst := constantFoldDeclarationAst(expression, nil, false); folded {
			replacement := candidate
			replacement.Root = candidate.Root[1:]
			setArbitraryValue(&replacement, ValueToCss(foldedAst))
			same, known := canonicalizer.hasSameSignature(candidate, replacement)
			if !known {
				return candidate, false
			}
			if same {
				candidate, valueAst = replacement, foldedAst
			}
		}
	}

	if isLone(valueAst, "calc") {
		arguments := valueAst[0].Nodes
		if len(arguments) == 5 &&
			arguments[1].Kind == ValueNodeKindSeparator && arguments[1].Value == " " &&
			arguments[2].Kind == ValueNodeKindWord && arguments[2].Value == "*" &&
			arguments[3].Kind == ValueNodeKindSeparator && arguments[3].Value == " " {
			var operand *ValueNode
			switch {
			case arguments[4].Kind == ValueNodeKindWord && arguments[4].Value == "-1":
				operand = &arguments[0]
			case arguments[0].Kind == ValueNodeKindWord && arguments[0].Value == "-1":
				operand = &arguments[4]
			}
			if operand != nil {
				replacement := candidate
				replacement.Root = "-" + candidate.Root
				setArbitraryValue(&replacement, ValueToCss([]ValueNode{*operand}))
				same, known := canonicalizer.hasSameSignature(candidate, replacement)
				if !known {
					return candidate, false
				}
				if same {
					candidate = replacement
				}
			}
		}
	}
	return candidate, true
}

// hasSameSignature is upstream's comparison through the exact-equal relation: true only when both sign
// exactly and alike. Known is false when either signature is unknown, since upstream might answer
// either way, and the caller then aborts.
func (canonicalizer *Canonicalizer) hasSameSignature(left ParsedCandidate, right ParsedCandidate) (same bool, known bool) {
	prefix := canonicalizer.system.Prefix()
	leftSignature, leftState := canonicalizer.classSignature(PrintCandidate(left, prefix))
	rightSignature, rightState := canonicalizer.classSignature(PrintCandidate(right, prefix))
	if leftState == SignatureUnknown || rightState == SignatureUnknown {
		return false, false
	}
	if leftState != SignatureExact || rightState != SignatureExact {
		return false, true
	}
	return leftSignature == rightSignature, true
}

// arbitraryUtilities is upstream's pass of the same name, answered only when every class upstream would
// weigh is known here.
func (canonicalizer *Canonicalizer) arbitraryUtilities(candidate ParsedCandidate) (ParsedCandidate, bool) {
	value, isArbitrary := arbitraryValueOf(&candidate)
	if !isArbitrary {
		return candidate, true
	}
	if candidate.Modifier != nil {
		return candidate, false
	}
	index := canonicalizer.shared.classIndex()
	if index.redeclared {
		return candidate, false
	}

	prefix := canonicalizer.system.Prefix()
	target := PrintCandidate(candidate, prefix)
	targetSignature, state := canonicalizer.classSignature(target)
	if state != SignatureExact {
		return candidate, false
	}
	properties := SignatureProperties(targetSignature)

	pool := canonicalizer.pool(properties)
	if pool.uncertain {
		return candidate, false
	}

	matches := pool.exact[targetSignature]
	replacement := ""
	switch {
	case len(matches) > 1:
		// Several classes sign alike: upstream takes the one non-negative class, and bails on two.
		for _, match := range matches {
			if strings.HasPrefix(match, "-") {
				continue
			}
			if replacement != "" {
				return candidate, true
			}
			replacement = match
		}
		if replacement == "" {
			return candidate, true
		}
	case len(matches) == 1:
		replacement = matches[0]
	default:
		return canonicalizer.arbitraryFallback(candidate, value, targetSignature, properties)
	}

	return canonicalizer.replacedWith(candidate, value, replacement)
}

// replacedWith is the rewrite to replacement when allVariablesAreUsed allows it, the candidate unchanged
// when it does not, and an abort when that cannot be told or the replacement is an arbitrary value, which
// upstream's later passes arbitraryValueToBareValueUtility and dropUnnecessaryDataTypes, not ported, may
// rewrite again.
func (canonicalizer *Canonicalizer) replacedWith(candidate ParsedCandidate, value string, replacement string) (ParsedCandidate, bool) {
	used, known := canonicalizer.allVariablesAreUsed(value, replacement)
	if !known {
		return candidate, false
	}
	if !used {
		return candidate, true
	}
	readings := ParseCandidate(canonicalizer.prefixed(replacement), canonicalizer.system)
	if len(readings) == 0 {
		return candidate, false
	}
	if _, isArbitrary := arbitraryValueOf(&readings[0]); isArbitrary {
		return candidate, false
	}
	return readings[0], true
}

// arbitraryFallback is upstream's search when no listed class shares the signature: every functional
// root, with the value as a bare value, as a multiple of `--spacing`, and as an arbitrary value, the first
// that signs alike winning. Upstream walks roots in registration order. This walks them in no order and
// declines when two roots both match, or when any try it cannot sign could, so the winner never depends
// on the order.
func (canonicalizer *Canonicalizer) arbitraryFallback(candidate ParsedCandidate, value string, targetSignature string, properties map[string]bool) (ParsedCandidate, bool) {
	index := canonicalizer.shared.classIndex()
	compiler := index.compiler

	multiplier, hasMultiplier := index.spacingMultiple(value)
	rootPrefix := ""
	if hasMultiplier && multiplier < 0 {
		rootPrefix = "-"
		multiplier = -multiplier
	}
	reasonable := hasMultiplier && isValidSpacingMultiplier(javaScriptNumberString(multiplier)) && index.isReasonableBareValue(multiplier)

	matched := ""
	for _, registered := range index.roots {
		root := rootPrefix + registered
		if !rootCouldSign(root, properties) {
			continue
		}
		tries := []string{root + "-" + value}
		if reasonable {
			tries = append(tries, root+"-"+javaScriptNumberString(multiplier))
		}
		tries = append(tries, root+"-["+value+"]")

		rootMatch := ""
	tries:
		for _, try := range tries {
			for _, reading := range ParseCandidate(canonicalizer.prefixed(try), canonicalizer.system) {
				printed := PrintCandidate(reading, canonicalizer.system.Prefix())
				signature, state := canonicalizer.classSignature(printed)
				switch state {
				case SignatureExact:
					if signature != targetSignature {
						continue
					}
					used, known := canonicalizer.allVariablesAreUsed(value, printed)
					if !known {
						return candidate, false
					}
					if used {
						rootMatch = printed
						break tries
					}
				case SignatureUnknown:
					// possible-match: a try this port cannot sign could be upstream's winner.
					if compiler.ClassSignatureBound(printed, canonicalizer.features).Admits(properties) {
						return candidate, false
					}
				}
			}
		}
		if rootMatch == "" {
			continue
		}
		if matched != "" {
			return candidate, false
		}
		matched = rootMatch
	}
	if matched == "" {
		return candidate, true
	}
	return canonicalizer.replacedWith(candidate, value, matched)
}

// rootCouldSign is the possible-match relation for a whole root: a framework root whose probed universe
// does not hold every property cannot sign to them, whatever its value. A root with no universe, a
// repository one or one no probe reached, is always tried.
func rootCouldSign(root string, properties map[string]bool) bool {
	universe, probed := FrameworkRootProperties[root]
	if !probed {
		return true
	}
	all := map[string]bool{}
	for _, list := range [][]string{universe.Colour, universe.Other, universe.Arbitrary} {
		for _, property := range list {
			all[property] = true
		}
	}
	for property := range properties {
		if !all[property] {
			return false
		}
	}
	return true
}

// spacingMultiple is upstream's spacing cache for one value: the value over the theme's `--spacing`, when
// both fold to dimensions in the same unit and the spacing is not zero.
func (index *classListIndex) spacingMultiple(value string) (float64, bool) {
	if index.spacing == nil || index.spacing.value == 0 {
		return 0, false
	}
	parsed := parseDimension(constantFoldDeclaration(value, nil))
	if parsed == nil || parsed.unit != index.spacing.unit {
		return 0, false
	}
	return parsed.value / index.spacing.value, true
}

// isReasonableBareValue is upstream's isReasonableBareValue: a multiple whose length stays within the
// largest breakpoint, 1536px or 96rem.
func (index *classListIndex) isReasonableBareValue(multiplier float64) bool {
	if index.spacing == nil {
		return false
	}
	length := multiplier * index.spacing.value
	switch index.spacing.unit {
	case "px":
		return length <= 1536
	case "rem":
		return length <= 1536/16
	}
	return false
}

// cssVariableName is a variable name that can be searched for as written. Upstream builds a regular
// expression from the name without escaping it, so a name holding anything else would match differently
// there than a literal search does here.
var cssVariableName = regexp.MustCompile(`^--[A-Za-z0-9_-]+$`)

// allVariablesAreUsed is upstream's function of the same name: a replacement for a value using CSS
// variables is safe only when its compiled CSS still uses each of them and sets none of them. Known is
// false when the replacement cannot be compiled exactly, or a variable cannot be searched for faithfully.
func (canonicalizer *Canonicalizer) allVariablesAreUsed(value string, replacement string) (used bool, known bool) {
	if !strings.Contains(value, "var(--") {
		return true, true
	}
	var variables []string
	var collect func(nodes []ValueNode)
	collect = func(nodes []ValueNode) {
		for index := range nodes {
			node := &nodes[index]
			if node.Kind == ValueNodeKindFunction && node.Value == "var" && len(node.Nodes) > 0 {
				variables = append(variables, node.Nodes[0].Value)
			}
			collect(node.Nodes)
		}
	}
	collect(ParseValue(value))

	index := canonicalizer.shared.classIndex()
	nodes, state := index.rawCompiler.signatureNodes(replacement)
	if state != SignatureExact || !substituteThemeFunctions(nodes, index.rawCompiler.compileTheme) {
		return false, false
	}
	var css strings.Builder
	printSignatureCss(&css, StyleRule(".x", nodes...), 0)
	text := css.String()

	for _, variable := range variables {
		if !cssVariableName.MatchString(variable) {
			return false, false
		}
		if !regexp.MustCompile(`var\(`+variable+`[,)]`).MatchString(text) || strings.Contains(text, variable+":") {
			return false, true
		}
	}
	return true, true
}
