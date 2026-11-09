// Package function_name compares a function's full name with a name, in place: whether types.Func's FullName,
// or honnef.co/go/tools' typeutil.FuncName, which spells it the same way, would equal it, without building the
// string. A rule asks it of every call it hears, and most calls are not the function it wants, so building the
// name for each was the rule's cost (#p92j24d, #bq6gxwy: about 1.2 CPU-s of a cold run at cohere's root).
package function_name

import (
	"go/types"
	"strings"
)

// Matches is whether function's full name is name, as types.Func's FullName and typeutil.FuncName spell it: its
// name alone when it has no type yet, a method's receiver in parentheses, a dot and its name, and any other
// function's package path, a dot and its name, or its name alone when it has no package or its package no path.
// Only a method's receiver needs printing, and only once name has a method's shape and ends in its own name.
func Matches(function *types.Func, name string) bool {
	if function.Type() == nil {
		return name == function.Name()
	}
	if function.Signature().Recv() != nil {
		receiver, isMethodName := strings.CutSuffix(name, function.Name())
		if !isMethodName || !strings.HasPrefix(receiver, "(") || !strings.HasSuffix(receiver, ").") {
			return false
		}
		return function.FullName() == name
	}
	if function.Pkg() == nil || function.Pkg().Path() == "" {
		return name == function.Name()
	}
	path := function.Pkg().Path()
	return len(name) == len(path)+1+len(function.Name()) && strings.HasPrefix(name, path) &&
		name[len(path)] == '.' && strings.HasSuffix(name, function.Name())
}

// Index is which of names function's full name is, the first in their order, or -1 when it is none of them, as
// a switch over FullName or a lookup by it would answer.
func Index(function *types.Func, names ...string) int {
	for index, name := range names {
		if Matches(function, name) {
			return index
		}
	}
	return -1
}
