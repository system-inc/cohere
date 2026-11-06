package flagged

type report struct {
	Result  int
	value   string
	Outcome string
}

func (holder report) State() string { return holder.value }

func (holder report) Value() string { return holder.value }

type wrapper struct{ Data []byte }

type machine struct{ State string }

// The exported field and the unexported one stutter alike, since the variable's first letter is lower either way.
func localField() (int, string) {
	result := report{Result: 1}
	value := report{value: "a"}
	return result.Result, value.value // want `"result.Result" stutters` `"value.value" stutters`
}

// Nested in a comparison, which is where these actually appear. The want spells out Go's words for the member.
func comparison(outcome report) bool {
	// want +1 `^"outcome\.Outcome" stutters, .* repeats the field or method instead of saying which outcome this is\.`
	return outcome.Outcome == "Unreadable"
}

func methodCall(state report) string {
	return state.State() // want `"state.State" stutters`
}

// A method value, not called, is the same member reached through the same name.
func methodValue(value report) func() string {
	return value.Value // want `"value.Value" stutters`
}

// A receiver's name is the code's own choice, as any parameter's is.
func (state *machine) describe() string {
	return state.State // want `"state.State" stutters`
}

func assignedTo(result *report) {
	result.Result = 2 // want `"result.Result" stutters`
}

func rangeVariable(items []wrapper) int {
	total := 0
	for _, data := range items {
		total += len(data.Data) // want `"data.Data" stutters`
	}
	return total
}

func closureParameter() func(wrapper) int {
	return func(data wrapper) int {
		return len(data.Data) // want `"data.Data" stutters`
	}
}

func parenthesizedAndDereferenced(result *report) (int, int) {
	return (result).Result, (*result).Result // want `"result.Result" stutters` `"result.Result" stutters`
}

type envelope struct{ Response int }

// A package variable is declared here as much as a local is, and is read further from its declaration.
var response = envelope{Response: 3}

func packageVariable() int {
	return response.Response // want `"response.Response" stutters`
}

// A variable of a type parameter is still a variable this package named.
func typeParameter[Item interface{ Item() int }](item Item) int {
	return item.Item() // want `"item.Item" stutters`
}

// An embedded field is named for its type, and the variable reaching it borrows that word all the same.
type Thing struct{ size int }

type box struct{ Thing }

func embedded(thing box) int {
	return thing.Thing.size // want `"thing.Thing" stutters`
}
