package lookalike

// value is a package variable that shares a parameter's name, so only resolving by object tells them apart.
var value int

type handler func(value int) int

type doer interface {
	Do(value int) int
}

func takesValue(value int) int { return value }

func assignsPackageVariable() *int {
	value = 2
	value++
	return &value
}

func assignsLocal() *int {
	value := 1
	value = 2
	value += 3
	return &value
}

// A parameter of a sibling literal has the same name as the local here, and they are different variables.
func siblingLiterals() int {
	first := func(value int) int { return value }
	result := 0
	result = first(result)
	second := func() int {
		result := 5
		result++
		return result
	}
	return result + second()
}

// A literal's own local named like the outer parameter is a new variable, so the parameter keeps its value.
func literalLocal(value int) int {
	compute := func() int {
		value := 9
		value--
		return value
	}
	return value + compute()
}

// A method of an interface or a function type names parameters nothing can assign.
func usesTypes(run handler, target doer) int {
	return run(1) + target.Do(2)
}
