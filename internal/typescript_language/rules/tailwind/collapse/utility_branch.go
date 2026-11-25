package tailwind

// UtilityBranch is what one of these handle bodies reads before it picks an arm.
//
// Four fields rather than a resolved value, for the reason in the package comment: the shadow family
// branches on the modifier, which resolution consumes and does not report.
//
// The zero value is a class written with no value at all, which is a real state here and the one
// several of these roots answer differently from every other: `border` is a width, `border-red-500`
// is a colour.
type UtilityBranch struct {
	// HasValue is false for a class written as the bare root, such as `border` or `ring`.
	HasValue bool
	// IsArbitrary is true for a bracketed value, such as `border-[3px]`.
	//
	// Separate from DataType because it decides an arm on its own for three roots. `scale-[2]`
	// returns from upstream's arbitrary arm with one declaration while `scale-150` falls through to
	// four, and both infer the same type.
	IsArbitrary bool
	// DataType is the value's inferred type, or the empty string when it infers as nothing.
	//
	// Callers get this from InferDataType against the root's own ordered type list, or from the
	// author's own annotation on a value written as `bg-[color:var(--x)]`.
	DataType DataType
	// ResolvedAsColor is whether a named value resolved through a colour namespace.
	//
	// An input rather than something derived here. Upstream this is `resolveThemeColor` returning a
	// value, which consults the repository's own `--color` keys; deriving it in this file would put
	// one repository's tokens in a table describing Tailwind.
	ResolvedAsColor bool
	// HasModifier is whether the class carried a `/...` modifier.
	//
	// The shadow family writes an alpha declaration whose value is absent without one and present
	// with one, and PropertySort skips the first and counts the second. `text` uses it differently
	// again: a modifier turns a font size into a font size plus a line height.
	HasModifier bool
	// ResolvedNamespace is the theme namespace a named value resolved through, when one that is not
	// a colour decides an arm.
	//
	// `ResolvedAsColor` covers the colour namespaces, which is the split most of these roots make.
	// One root splits on a namespace that is not a colour: `font` consults `--font` before
	// `--font-weight`, so `font-mono` emits `font-family` and `font-medium` emits `font-weight`,
	// and both are named values that infer as nothing. That is the branch `ClassDeclaredProperties`
	// recorded as three per-class overrides.
	//
	// An input for the same reason `ResolvedAsColor` is: which keys are in `--font` is this
	// repository's `@theme`, not Tailwind's.
	ResolvedNamespace string
	// Value is the resolved value the class carried, when a caller needs the declarations to differ
	// between two values of one root.
	//
	// Empty for every caller that reads property names, which is all of them but one: the branch
	// decides which arm runs and the arm decides the properties, and neither consults this. It
	// exists for `ComposesFor`, which emits a root twice and asks whether the declarations an author
	// can see came out the same, and that question is meaningless if both emissions carry one
	// sentinel.
	//
	// A root's own `--tw-*` variables take this. The shared property its family layers through does
	// not: `box-shadow` carries a constant naming every shadow variable whatever the value was, and
	// that constant is what makes two shadows stack rather than overwrite. Emitting the value there
	// instead is exactly the flattening that made composition unanswerable from these bodies.
	Value string
}

// isOneOf reports whether the branch's type is any of the given ones.
//
// Upstream these arms are `switch` cases that fall through to one body, as in `case 'length': case
// 'line-width':`. Written as a predicate rather than a Go switch so an arm reads as the set of types
// it accepts, which is how the source spells it.
func (branch UtilityBranch) isOneOf(types ...DataType) bool {
	for _, dataType := range types {
		if branch.DataType == dataType {
			return true
		}
	}
	return false
}

// maskStopIsColor is the shared dispatcher in `maskStopUtility`, which all 18 stop roots run.
//
// An arbitrary value is a colour when it infers as one, against the list `['length', 'percentage',
// 'color']`; a named value is a colour when the theme's colour namespaces answered it, and a
// position otherwise. Both halves are read off the branch rather than recomputed, for the reason in
// the package comment.
func (branch UtilityBranch) maskStopIsColor() bool {
	if branch.IsArbitrary {
		return branch.DataType == DataTypeColor
	}
	return branch.ResolvedAsColor
}

// shadowIsColor is the shadow family's own type question.
//
// The arbitrary arm infers against the single-element list `['color']`, so anything that is not a
// colour falls to the size arm. A named value is a colour when the theme's colour namespaces
// answered it.
func (branch UtilityBranch) shadowIsColor() bool {
	if branch.IsArbitrary {
		return branch.DataType == DataTypeColor
	}
	return branch.ResolvedAsColor
}
