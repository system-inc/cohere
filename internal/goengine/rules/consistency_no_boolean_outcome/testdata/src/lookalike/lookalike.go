package lookalike

// A flag named off the twins' list says something other than how an operation turned out.
type Task struct {
	Done  bool
	Error error
}

type Matched struct {
	Matched bool
	Value   string
}

// Go's spelling of the acronym is not on the twins' list, and the house spells it Ok, so OK is a known miss.
type Status struct {
	OK    bool
	Value string
}

// A companion named off the list does not make the flag an envelope.
type Coded struct {
	Ok        bool
	ErrorCode int
}

type Said struct {
	IsFailure bool
	Said      string
}

// A flag that is not the predeclared bool is not the bare two-valued flag: a pointer, a named type over bool, and a
// type parameter, however it is constrained.
type MaybeOk struct {
	Ok    *bool
	Error error
}

type Flag bool

type Named struct {
	Ok    Flag
	Error error
}

type Constrained[Bit ~bool] struct {
	Ok    Bit
	Error error
}

// A local type named bool shadows the predeclared one, so a field of it is no flag.
func shadowed() any {
	type bool int
	type local struct {
		ok    bool
		error string
	}
	return local{}
}

// A companion promoted from an embedded struct is that struct's field, not one this struct declares.
type Base struct {
	Error error
}

type Derived struct {
	Base
	Ok bool
}

// A method is not a field, so Error here is behavior beside state.
type Probe struct {
	Ok bool
}

func (probe Probe) Error() string { return "" }

// An interface's methods are a contract, not a field set.
type Reporter interface {
	Ok() bool
	Error() string
}

// A struct spelled inline is not a declaration a reader looks the shape up by, as the twins leave an inline object
// type alone: a var's type, an element type, a function's result, and a composite literal's type.
var reply struct {
	Ok    bool
	Error error
}

var replies []struct {
	Ok    bool
	Value int
}

func fetch() struct {
	Ok    bool
	Value int
} {
	return struct {
		Ok    bool
		Value int
	}{}
}

// A parenthesized type is not the bare literal, as both twins read only the bare one.
type Wrapped (struct {
	Ok    bool
	Error error
})

// Comma-ok is a pair of values, not fields.
func read(values map[string]string) string {
	value, ok := values["key"]
	if !ok {
		return ""
	}
	_ = reply
	_ = replies
	_ = fetch()
	return value
}
