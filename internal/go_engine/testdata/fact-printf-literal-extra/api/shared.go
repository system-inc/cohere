// Package api declares three variables holding a function literal that hands its format and values to emit, in a
// file every build compiles. printf finds such a literal a wrapper, its fact on the variable, where emit is one, and
// each platform's file declares its own emit, which forwards to fmt.Printf in one build alone. So each variable is a
// printf wrapper in exactly that build, its text and file the same in each. Logf declares no type, so it takes its
// type from its literal, which uses emit; TypedLogf states its type; and AssignedLogf is given its literal in init.
package api

// Logf hands its format and values to emit.
var Logf = func(format string, args ...any) { emit(format, args...) }

// TypedLogf hands its format and values to emit, its type stated.
var TypedLogf func(format string, args ...any) = func(format string, args ...any) { emit(format, args...) }

// AssignedLogf is given a literal that hands its format and values to emit in init.
var AssignedLogf func(format string, args ...any)

func init() {
	AssignedLogf = func(format string, args ...any) { emit(format, args...) }
}

// Use hands Logf a string for %d, a printf finding in exactly the builds where Logf is a wrapper.
func Use() { Logf("%d", "x") }
