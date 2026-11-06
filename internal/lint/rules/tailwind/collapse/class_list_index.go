package tailwind

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
