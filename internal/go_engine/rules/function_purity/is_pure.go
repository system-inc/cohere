package function_purity

// IsPure marks a function judged pure. It prints as staticcheck's fact does, so the two compare by what they say.
type IsPure struct{}

func (*IsPure) AFact() {
	// AFact only marks the type as a fact, so it does nothing.
}

func (*IsPure) String() string { return "is pure" }
