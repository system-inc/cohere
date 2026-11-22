// Package api declares four variables holding a function literal that hands its format and values to emit, in a file
// every build compiles. printf finds such a literal a wrapper, its fact on the variable, where emit is one, and each
// platform's file declares its own emit, which forwards to fmt.Printf in one build alone. So each variable is a printf
// wrapper in exactly that build, its text and file the same in each. Logf declares no type, so it takes its type from
// its literal, which uses emit; TypedLogf states its type; and AssignedLogf and OwnLogf are given theirs in init.
// OwnUse calls OwnLogf, api's own, as no body of another package can be the pin of its fact's comparison
// (fact_reach.PushedObject.Local).
package api

import "example.com/printfliteralhost/values"

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

// OwnLogf is given a literal that hands its format and values to emit in init, its values spelled by values.Value.
var OwnLogf func(format string, args ...values.Value)

func init() {
	OwnLogf = func(format string, args ...values.Value) { emit(format, args...) }
}

// OwnUse hands OwnLogf, api's own, a string for %d, a printf finding in exactly the builds where OwnLogf is a wrapper.
// It names only OwnLogf, whose text and file are the same in every build.
func OwnUse() { OwnLogf("%d", "x") }
