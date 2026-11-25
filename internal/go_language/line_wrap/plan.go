package line_wrap

// plan is one construct broken at every one of its break points.
type plan struct {
	shape      Shape
	start, end int
	// openLine is the line of the opening bracket, or of the chain's first operand.
	openLine int
	breaks   []breakPoint
	// lastFunctionLiteral is set when the last element is a function literal spanning lines, a call people
	// rarely want exploded one argument per line.
	lastFunctionLiteral bool
}

func (plan plan) edits() []edit {
	var edits []edit
	for _, point := range plan.breaks {
		text := "\n"
		if point.trailingComma {
			text = ",\n"
		}
		edits = append(edits, edit{start: point.offset, end: point.offset, text: text})
	}
	return edits
}
